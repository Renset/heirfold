// Package faces finds faces in a picture and turns a face into a list of
// numbers that is close for two pictures of the same person. Both steps run
// on the server, in pure Go. No picture and no number leaves the server.
//
// The finder is YuNet (MIT License) and is inside the binary. The model for
// the numbers is SFace (Apache License 2.0). It is large, so it is a file
// that the operator puts next to the database. Both come from the OpenCV
// Zoo. The files in the folder models say more.
package faces

import (
	"crypto/sha256"
	_ "embed" // The finder model is a part of the binary.
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"sort"

	"github.com/Renset/heirfold/internal/nn"
)

//go:embed models/face_detection_yunet_2023mar.onnx
var finderFile []byte

const (
	// RecognizerFile is the name of the model that turns a face into numbers.
	RecognizerFile = "face_recognition_sface_2021dec.onnx"
	// RecognizerSHA256 is the checksum of that file. The server loads no other file.
	RecognizerSHA256 = "0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79"
	// VectorSize is the length of the list of numbers of one face.
	VectorSize = 128
	// SamePerson is the lowest likeness of two faces of one person that the
	// authors of the model give: the cosine of the two lists of numbers.
	SamePerson = 0.363

	// minScore is the lowest score of a face that the finder reports.
	minScore = 0.7
	// overlap is the share of common area above which two finds are one face.
	overlap  = 0.3
	faceSide = 112
)

// ErrModel means that the file of the recognition model is not the expected file.
var ErrModel = errors.New("faces: the model file is not the expected file")

// Face is one face in a picture. All places are parts of the width and of
// the height of the upright picture, from 0 to 1.
type Face struct {
	X, Y, W, H float64
	Score      float64
	// Marks are the eyes, the tip of the nose and the corners of the mouth.
	Marks [5][2]float64
}

// Engine holds the loaded models.
type Engine struct {
	finder     *nn.Model
	recognizer *nn.Model
}

// New loads the finder. If recognizerPath names a file, it also loads the
// recognition model, after a check of its checksum. An empty path, or a path
// to no file, gives an engine that only finds faces.
func New(recognizerPath string) (*Engine, error) {
	finder, err := nn.Load(finderFile)
	if err != nil {
		return nil, fmt.Errorf("faces: finder model: %w", err)
	}
	engine := &Engine{finder: finder}
	if recognizerPath == "" {
		return engine, nil
	}
	data, err := os.ReadFile(recognizerPath) // #nosec G304 -- the operator configures this path.
	if errors.Is(err, os.ErrNotExist) {
		return engine, nil
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != RecognizerSHA256 {
		return nil, ErrModel
	}
	if engine.recognizer, err = nn.Load(data); err != nil {
		return nil, fmt.Errorf("faces: recognition model: %w", err)
	}
	return engine, nil
}

// Recognizes reports whether the engine can compare faces.
func (e *Engine) Recognizes() bool { return e.recognizer != nil }

// planes returns the picture as a tensor of three colour planes with values
// from 0 to 255. The picture stands in the top left corner of a tensor of
// the given size; the rest is black.
func planes(img *image.RGBA, width, height, tensorW, tensorH int, order [3]int) *nn.Tensor {
	t := nn.NewTensor(1, 3, tensorH, tensorW)
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	plane := tensorW * tensorH
	for y := range height {
		// Each new pixel is the mean of the block of old pixels that it covers.
		y0, y1 := y*srcH/height, max((y+1)*srcH/height, y*srcH/height+1)
		for x := range width {
			x0, x1 := x*srcW/width, max((x+1)*srcW/width, x*srcW/width+1)
			var sum [3]int
			for sy := y0; sy < y1; sy++ {
				row := img.Pix[sy*img.Stride+x0*4 : sy*img.Stride+x1*4]
				for i := 0; i < len(row); i += 4 {
					sum[0] += int(row[i])
					sum[1] += int(row[i+1])
					sum[2] += int(row[i+2])
				}
			}
			count := float32((y1 - y0) * (x1 - x0))
			at := y*tensorW + x
			for c := range 3 {
				t.Data[c*plane+at] = float32(sum[order[c]]) / count
			}
		}
	}
	return t
}

func clamp01(v float32) float64 {
	return math.Min(math.Max(float64(v), 0), 1)
}

// findAt runs the finder on the picture with the longest side reduced to side.
func (e *Engine) findAt(img *image.RGBA, side int) ([]Face, error) {
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW < 16 || srcH < 16 {
		return nil, nil
	}
	scale := math.Min(1, float64(side)/float64(max(srcW, srcH)))
	width, height := max(1, int(math.Round(float64(srcW)*scale))), max(1, int(math.Round(float64(srcH)*scale)))
	// The network halves the picture five times.
	tensorW, tensorH := (width+31)/32*32, (height+31)/32*32
	// The finder wants blue, green, red.
	results, err := e.finder.Run(planes(img, width, height, tensorW, tensorH, [3]int{2, 1, 0}))
	if err != nil {
		return nil, err
	}
	var found []Face
	for _, stride := range []int{8, 16, 32} {
		name := fmt.Sprint(stride)
		cls, obj, box, kps := results["cls_"+name], results["obj_"+name], results["bbox_"+name], results["kps_"+name]
		columns, rows := tensorW/stride, tensorH/stride
		if cls == nil || obj == nil || box == nil || kps == nil || len(cls.Data) != columns*rows ||
			len(obj.Data) != columns*rows || len(box.Data) != columns*rows*4 || len(kps.Data) != columns*rows*10 {
			return nil, fmt.Errorf("faces: the finder gave results of another size for the step %d", stride)
		}
		s := float64(stride)
		for r := range rows {
			for c := range columns {
				i := r*columns + c
				score := math.Sqrt(clamp01(cls.Data[i]) * clamp01(obj.Data[i]))
				if score < minScore {
					continue
				}
				w := math.Exp(float64(box.Data[i*4+2])) * s
				h := math.Exp(float64(box.Data[i*4+3])) * s
				cx := (float64(c) + float64(box.Data[i*4])) * s
				cy := (float64(r) + float64(box.Data[i*4+1])) * s
				face := Face{
					X: (cx - w/2) / float64(width), Y: (cy - h/2) / float64(height),
					W: w / float64(width), H: h / float64(height), Score: score,
				}
				for k := range 5 {
					face.Marks[k] = [2]float64{
						(float64(kps.Data[i*10+2*k]) + float64(c)) * s / float64(width),
						(float64(kps.Data[i*10+2*k+1]) + float64(r)) * s / float64(height),
					}
				}
				found = append(found, face)
			}
		}
	}
	return found, nil
}

// common returns the share of the common area of two faces in their joint area.
func common(a, b Face) float64 {
	w := math.Min(a.X+a.W, b.X+b.W) - math.Max(a.X, b.X)
	h := math.Min(a.Y+a.H, b.Y+b.H) - math.Max(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	inter := w * h
	return inter / (a.W*a.H + b.W*b.H - inter)
}

// unique keeps the best find of each face.
func unique(found []Face) []Face {
	sort.SliceStable(found, func(i, j int) bool { return found[i].Score > found[j].Score })
	var out []Face
	for _, face := range found {
		keep := true
		for _, other := range out {
			if common(face, other) > overlap {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, face)
		}
	}
	return out
}

// Find returns the faces of the picture, the surest first. It looks at the
// picture at two sizes: the finder sees faces from about 10 to 300 pixels,
// so the large size finds the small faces of a group and the small size
// finds a face that fills the picture.
func (e *Engine) Find(img *image.RGBA) ([]Face, error) {
	var all []Face
	for _, side := range []int{960, 320} {
		found, err := e.findAt(img, side)
		if err != nil {
			return nil, err
		}
		// A second pass of the same size adds nothing.
		all = append(all, found...)
		if max(img.Bounds().Dx(), img.Bounds().Dy()) <= 320 {
			break
		}
	}
	faces := unique(all)
	// A face must lie in the picture for the most part.
	out := faces[:0]
	for _, face := range faces {
		if face.W > 0 && face.H > 0 && face.X+face.W/2 > 0 && face.X+face.W/2 < 1 && face.Y+face.H/2 > 0 && face.Y+face.H/2 < 1 {
			out = append(out, face)
		}
	}
	return out, nil
}

// template holds the places of the five marks in the picture of 112 by 112
// pixels that the recognition model expects.
var template = [5][2]float64{
	{38.2946, 51.6963}, {73.5318, 51.5014}, {56.0252, 71.7366}, {41.5493, 92.3655}, {70.7299, 92.2041},
}

// align returns the face as a tensor of 112 by 112 pixels with the eyes, the
// nose and the mouth at the places of the template. It turns, moves and
// scales the picture, and it does not bend it.
func align(img *image.RGBA, face Face) *nn.Tensor {
	bounds := img.Bounds()
	width, height := float64(bounds.Dx()), float64(bounds.Dy())
	// The best turn, scale and move from the marks to the template, by least squares.
	var meanX, meanY, toX, toY float64
	for k := range 5 {
		meanX += face.Marks[k][0] * width / 5
		meanY += face.Marks[k][1] * height / 5
		toX += template[k][0] / 5
		toY += template[k][1] / 5
	}
	var a, b, spread float64
	for k := range 5 {
		x, y := face.Marks[k][0]*width-meanX, face.Marks[k][1]*height-meanY
		u, v := template[k][0]-toX, template[k][1]-toY
		a += x*u + y*v
		b += x*v - y*u
		spread += x*x + y*y
	}
	t := nn.NewTensor(1, 3, faceSide, faceSide)
	if spread == 0 || (a == 0 && b == 0) {
		return t
	}
	a, b = a/spread, b/spread
	moveX, moveY := toX-(a*meanX-b*meanY), toY-(b*meanX+a*meanY)
	// The way back: from a pixel of the result to a place in the picture.
	size := a*a + b*b
	plane := faceSide * faceSide
	for v := range faceSide {
		for u := range faceSide {
			du, dv := float64(u)-moveX, float64(v)-moveY
			x := (a*du + b*dv) / size
			y := (-b*du + a*dv) / size
			x0, y0 := int(math.Floor(x)), int(math.Floor(y))
			fx, fy := float32(x-float64(x0)), float32(y-float64(y0))
			var sum [3]float32
			for _, corner := range [4][3]float32{{0, 0, (1 - fx) * (1 - fy)}, {1, 0, fx * (1 - fy)}, {0, 1, (1 - fx) * fy}, {1, 1, fx * fy}} {
				px, py := x0+int(corner[0]), y0+int(corner[1])
				if px < 0 || py < 0 || px >= bounds.Dx() || py >= bounds.Dy() {
					continue
				}
				at := py*img.Stride + px*4
				sum[0] += corner[2] * float32(img.Pix[at])
				sum[1] += corner[2] * float32(img.Pix[at+1])
				sum[2] += corner[2] * float32(img.Pix[at+2])
			}
			at := v*faceSide + u
			// The model wants red, green, blue.
			t.Data[at], t.Data[plane+at], t.Data[2*plane+at] = sum[0], sum[1], sum[2]
		}
	}
	return t
}

// Vector returns the list of numbers of a face, with the length 1. The
// likeness of two faces is the sum of the products of their numbers.
func (e *Engine) Vector(img *image.RGBA, face Face) ([]float32, error) {
	if e.recognizer == nil {
		return nil, errors.New("faces: no recognition model")
	}
	results, err := e.recognizer.Run(align(img, face))
	if err != nil {
		return nil, err
	}
	out := results[e.recognizer.Outputs()[0]]
	if out == nil || len(out.Data) != VectorSize {
		return nil, errors.New("faces: the recognition model gave a result of another size")
	}
	var length float64
	for _, v := range out.Data {
		length += float64(v) * float64(v)
	}
	length = math.Sqrt(length)
	if length == 0 {
		return nil, errors.New("faces: the recognition model gave only zeros")
	}
	vector := make([]float32, VectorSize)
	for i, v := range out.Data {
		vector[i] = float32(float64(v) / length)
	}
	return vector, nil
}

// Likeness returns the cosine of two lists of numbers of the length 1.
func Likeness(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
