package faces

import (
	"encoding/json"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type drawnFace struct {
	Box    [4]float64  `json:"box"`
	Marks  [10]float64 `json:"marks"`
	Score  float64     `json:"score"`
	Vector []float32   `json:"vector"`
}

// drawn returns the test picture of invented faces and what OpenCV finds on it.
func drawn(t *testing.T) (*image.RGBA, []drawnFace) {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", "drawn.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	data, err := os.ReadFile(filepath.Join("testdata", "drawn.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Faces []drawnFace `json:"faces"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	sort.Slice(want.Faces, func(i, j int) bool { return want.Faces[i].Box[0] < want.Faces[j].Box[0] })
	return img, want.Faces
}

func engine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(filepath.Join("models", RecognizerFile))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// The finder must give the faces that OpenCV gives for the same picture.
func TestFindGivesTheFacesOfOpenCV(t *testing.T) {
	img, want := drawn(t)
	width, height := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	// The picture goes to the finder at its own size, as in the reference.
	found, err := engine(t).findAt(img, 640)
	if err != nil {
		t.Fatal(err)
	}
	found = unique(found)
	sort.Slice(found, func(i, j int) bool { return found[i].X < found[j].X })
	if len(found) != len(want) || len(want) != 7 {
		t.Fatalf("%d faces, OpenCV finds %d", len(found), len(want))
	}
	for i, face := range found {
		ref := want[i]
		got := [4]float64{face.X * width, face.Y * height, face.W * width, face.H * height}
		for k := range 4 {
			if math.Abs(got[k]-ref.Box[k]) > 0.2 {
				t.Errorf("face %d: box %v, want %v", i, got, ref.Box)
				break
			}
		}
		if math.Abs(face.Score-ref.Score) > 0.002 {
			t.Errorf("face %d: score %.4f, want %.4f", i, face.Score, ref.Score)
		}
		for k := range 5 {
			if math.Abs(face.Marks[k][0]*width-ref.Marks[2*k]) > 0.2 || math.Abs(face.Marks[k][1]*height-ref.Marks[2*k+1]) > 0.2 {
				t.Errorf("face %d: mark %d at %.2f, %.2f, want %.2f, %.2f", i, k,
					face.Marks[k][0]*width, face.Marks[k][1]*height, ref.Marks[2*k], ref.Marks[2*k+1])
			}
		}
	}
}

// Find looks at two sizes and still gives each face once.
func TestFindGivesEachFaceOnce(t *testing.T) {
	img, want := drawn(t)
	found, err := engine(t).Find(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != len(want) {
		t.Fatalf("%d faces, want %d", len(found), len(want))
	}
	for i, face := range found {
		if i > 0 && face.Score > found[i-1].Score {
			t.Fatal("the surest face is not first")
		}
		if face.X < 0 || face.Y < 0 || face.X+face.W > 1 || face.Y+face.H > 1 {
			t.Errorf("face %d is outside the picture: %+v", i, face)
		}
	}
	// A picture with no face, and a picture that is too small.
	blank := image.NewRGBA(image.Rect(0, 0, 200, 150))
	if found, err := engine(t).Find(blank); err != nil || len(found) != 0 {
		t.Fatalf("an empty picture: %v %v", found, err)
	}
	if found, err := engine(t).Find(image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil || len(found) != 0 {
		t.Fatalf("a tiny picture: %v %v", found, err)
	}
}

// The numbers of a face must be those of OpenCV. Two faces are alike or not
// in the same way in both programs.
func TestVectorGivesTheNumbersOfOpenCV(t *testing.T) {
	e := engine(t)
	if !e.Recognizes() {
		t.Skip("the recognition model is not here: it is large and is not in the repository")
	}
	img, want := drawn(t)
	found, err := e.findAt(img, 640)
	if err != nil {
		t.Fatal(err)
	}
	found = unique(found)
	sort.Slice(found, func(i, j int) bool { return found[i].X < found[j].X })
	vectors := make([][]float32, len(found))
	for i, face := range found {
		if vectors[i], err = e.Vector(img, face); err != nil {
			t.Fatal(err)
		}
		if got := Likeness(vectors[i], vectors[i]); math.Abs(got-1) > 1e-4 {
			t.Fatalf("the length of the numbers is %.5f", got)
		}
		if got := Likeness(vectors[i], want[i].Vector); got < 0.995 {
			t.Errorf("face %d: likeness with the numbers of OpenCV %.4f", i, got)
		}
	}
	for i := range vectors {
		for j := i + 1; j < len(vectors); j++ {
			ours, theirs := Likeness(vectors[i], vectors[j]), Likeness(want[i].Vector, want[j].Vector)
			if math.Abs(ours-theirs) > 0.03 {
				t.Errorf("faces %d and %d: likeness %.3f, OpenCV has %.3f", i, j, ours, theirs)
			}
		}
	}
}

func TestNewChecksTheRecognitionModel(t *testing.T) {
	// No file: the engine only finds faces.
	e, err := New(filepath.Join(t.TempDir(), RecognizerFile))
	if err != nil || e.Recognizes() {
		t.Fatalf("no file: %v", err)
	}
	if _, err := e.Vector(image.NewRGBA(image.Rect(0, 0, 50, 50)), Face{}); err == nil {
		t.Fatal("numbers without a model")
	}
	// Another file with the right name is refused.
	path := filepath.Join(t.TempDir(), RecognizerFile)
	if err := os.WriteFile(path, []byte("not the model"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path); !errors.Is(err, ErrModel) {
		t.Fatalf("a wrong file: %v", err)
	}
}
