package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// quadrants draws four plain colours: red and green on top, blue and white below.
func quadrants(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c color.RGBA
			switch {
			case x < w/2 && y < h/2:
				c = color.RGBA{R: 255, A: 255}
			case y < h/2:
				c = color.RGBA{G: 255, A: 255}
			case x < w/2:
				c = color.RGBA{B: 255, A: 255}
			default:
				c = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// near reports whether the pixel is close to the colour. JPEG is not exact.
func near(img image.Image, x, y int, want color.RGBA) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	diff := func(a uint32, b uint8) int {
		d := int(a>>8) - int(b)
		if d < 0 {
			d = -d
		}
		return d
	}
	return diff(r, want.R) < 40 && diff(g, want.G) < 40 && diff(b, want.B) < 40
}

var (
	red   = color.RGBA{R: 255, A: 255}
	green = color.RGBA{G: 255, A: 255}
	blue  = color.RGBA{B: 255, A: 255}
	white = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

func TestRenderShrinksAndKeepsTheShape(t *testing.T) {
	out, err := Render(bytes.NewReader(encodePNG(t, quadrants(400, 200))), Options{MaxSide: 100})
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 50 {
		t.Fatalf("size %v", img.Bounds())
	}
	if !near(img, 10, 10, red) || !near(img, 90, 10, green) || !near(img, 10, 40, blue) || !near(img, 90, 40, white) {
		t.Fatal("the colours moved")
	}
}

func TestRenderNeverEnlarges(t *testing.T) {
	out, err := Render(bytes.NewReader(encodePNG(t, quadrants(40, 20))), Options{MaxSide: 100})
	if err != nil {
		t.Fatal(err)
	}
	if img := decode(t, out); img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 {
		t.Fatalf("size %v", img.Bounds())
	}
}

func TestRenderCutsTheRegion(t *testing.T) {
	out, err := Render(bytes.NewReader(encodePNG(t, quadrants(200, 200))), Options{MaxSide: 50, Region: &Region{X1: 50, Y1: 0, X2: 100, Y2: 50}})
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	if img.Bounds().Dx() != 50 || img.Bounds().Dy() != 50 {
		t.Fatalf("size %v", img.Bounds())
	}
	if !near(img, 5, 5, green) || !near(img, 45, 45, green) {
		t.Fatal("the region is not the green quadrant")
	}
	// A region outside the image is ignored.
	out, err = Render(bytes.NewReader(encodePNG(t, quadrants(200, 200))), Options{MaxSide: 50, Region: &Region{X1: 60, Y1: 0, X2: 40, Y2: 500}})
	if err != nil {
		t.Fatal(err)
	}
	if img := decode(t, out); !near(img, 5, 5, red) {
		t.Fatal("a bad region must give the whole image")
	}
}

func TestRenderPutsTransparencyOnWhite(t *testing.T) {
	out, err := Render(bytes.NewReader(encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 20, 20)))), Options{MaxSide: 10})
	if err != nil {
		t.Fatal(err)
	}
	if img := decode(t, out); !near(img, 5, 5, white) {
		t.Fatal("a transparent image must become white")
	}
}

func TestRenderRefusesBadInput(t *testing.T) {
	if _, err := Render(bytes.NewReader([]byte("<svg xmlns='http://www.w3.org/2000/svg'/>")), Options{MaxSide: 10}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("text: %v", err)
	}
	if _, err := Render(bytes.NewReader(encodePNG(t, quadrants(100, 100))), Options{MaxSide: 10, MaxPixels: 5000}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("large image: %v", err)
	}
	// A header that promises a huge image must not cause a huge allocation.
	huge := encodePNG(t, quadrants(4, 4))
	binary.BigEndian.PutUint32(huge[16:20], 60000)
	binary.BigEndian.PutUint32(huge[20:24], 60000)
	if _, err := Render(bytes.NewReader(huge), Options{MaxSide: 10, MaxPixels: 40_000_000}); err == nil {
		t.Error("a huge header must fail")
	}
}

// withOrientation adds an EXIF block with the orientation to a JPEG.
func withOrientation(t *testing.T, img image.Image, orientation uint16, order binary.ByteOrder) []byte {
	t.Helper()
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 8+2+12+4)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1)
	order.PutUint16(tiff[10:], 0x0112)
	order.PutUint16(tiff[12:], 3)
	order.PutUint32(tiff[14:], 1)
	order.PutUint16(tiff[18:], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	out := append([]byte{0xFF, 0xD8}, segment...)
	out = append(out, payload...)
	return append(out, plain.Bytes()[2:]...)
}

func TestRenderFollowsTheCameraOrientation(t *testing.T) {
	source := quadrants(80, 40)
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		// Orientation 6 means: turn the stored image a quarter to the right.
		out, err := Render(bytes.NewReader(withOrientation(t, source, 6, order)), Options{MaxSide: 80})
		if err != nil {
			t.Fatal(err)
		}
		img := decode(t, out)
		if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 80 {
			t.Fatalf("size %v", img.Bounds())
		}
		if !near(img, 30, 10, red) || !near(img, 30, 70, green) || !near(img, 10, 10, blue) || !near(img, 10, 70, white) {
			t.Fatal("orientation 6 gave the wrong picture")
		}
		// Orientation 3 means: turn the image upside down.
		out, err = Render(bytes.NewReader(withOrientation(t, source, 3, order)), Options{MaxSide: 80})
		if err != nil {
			t.Fatal(err)
		}
		img = decode(t, out)
		if !near(img, 70, 30, red) || !near(img, 10, 10, white) {
			t.Fatal("orientation 3 gave the wrong picture")
		}
	}
}

func TestOrientationReaderSurvivesBrokenData(t *testing.T) {
	good := withOrientation(t, quadrants(8, 8), 6, binary.BigEndian)
	for cut := 0; cut < len(good) && cut < 80; cut++ {
		_ = exifOrientation(bytes.NewReader(good[:cut]))
	}
	for i := 2; i < 40 && i < len(good); i++ {
		broken := bytes.Clone(good)
		broken[i] ^= 0xFF
		if value := exifOrientation(bytes.NewReader(broken)); value < 1 || value > 8 {
			t.Fatalf("orientation %d", value)
		}
	}
}

func TestFitsCountsPixelsAndBytes(t *testing.T) {
	const pixels, bytes = 60_000_000, 256 << 20
	for name, tc := range map[string]struct {
		config image.Config
		want   bool
	}{
		"small RGBA":              {image.Config{ColorModel: color.RGBAModel, Width: 640, Height: 480}, true},
		"no width":                {image.Config{ColorModel: color.RGBAModel, Width: 0, Height: 480}, false},
		"negative height":         {image.Config{ColorModel: color.RGBAModel, Width: 640, Height: -1}, false},
		"just at the pixel limit": {image.Config{ColorModel: color.GrayModel, Width: 6000, Height: 10000}, true},
		"one row over the limit":  {image.Config{ColorModel: color.GrayModel, Width: 6000, Height: 10001}, false},
		// 60 Mpx of 8-bit RGBA are 240 MB, below the byte limit.
		"60 Mpx of 8-bit RGBA":  {image.Config{ColorModel: color.NRGBAModel, Width: 6000, Height: 10000}, true},
		"60 Mpx of YCbCr":       {image.Config{ColorModel: color.YCbCrModel, Width: 6000, Height: 10000}, true},
		"36 Mpx of gray":        {image.Config{ColorModel: color.GrayModel, Width: 6000, Height: 6000}, true},
		"16 Mpx of 16-bit RGBA": {image.Config{ColorModel: color.RGBA64Model, Width: 4000, Height: 4000}, true},
		// 33 554 432 pixels of 8 bytes are 256 MiB exactly.
		"16-bit RGBA at the byte limit": {image.Config{ColorModel: color.RGBA64Model, Width: 8192, Height: 4096}, true},
		"16-bit RGBA one row over":      {image.Config{ColorModel: color.RGBA64Model, Width: 8192, Height: 4097}, false},
		"36 Mpx of 16-bit RGBA":         {image.Config{ColorModel: color.NRGBA64Model, Width: 6000, Height: 6000}, false},
		"36 Mpx of 16-bit gray":         {image.Config{ColorModel: color.Gray16Model, Width: 6000, Height: 6000}, true},
		"36 Mpx with a palette":         {image.Config{ColorModel: color.Palette{color.Black}, Width: 6000, Height: 6000}, true},
		"sides that overflow int32":     {image.Config{ColorModel: color.GrayModel, Width: 1 << 30, Height: 1 << 30}, false},
	} {
		if got := Fits(tc.config, pixels, bytes); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
	// A model that the list does not name counts 4 bytes for each pixel.
	cmyk := image.Config{ColorModel: color.CMYKModel, Width: 8192, Height: 8192}
	if !Fits(cmyk, 0, 8192*8192*4) || Fits(cmyk, 0, 8192*8192*4-1) {
		t.Fatal("an unknown colour model must count 4 bytes for each pixel")
	}
	// A limit of 0 is no limit.
	huge := image.Config{ColorModel: color.RGBA64Model, Width: 1 << 20, Height: 1 << 20}
	if !Fits(huge, 0, 0) || Fits(huge, 0, bytes) || Fits(huge, pixels, 0) {
		t.Fatal("a limit of 0 must mean no limit")
	}
}

// A 16-bit PNG needs 8 bytes for each pixel, so Render refuses it before the
// decode when the bytes go over the limit.
func TestRenderRefusesAPictureOverTheMemoryLimit(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(0, 0, 100, 100))
	var file bytes.Buffer
	if err := png.Encode(&file, img); err != nil {
		t.Fatal(err)
	}
	// 100*100*8 = 80 000 bytes.
	if _, err := Render(bytes.NewReader(file.Bytes()), Options{MaxSide: 50, MaxBytes: 79_999}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := Render(bytes.NewReader(file.Bytes()), Options{MaxSide: 50, MaxBytes: 80_000}); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
}

// StoredPoint must be the reverse of the turn that orient does.
func TestStoredPointIsTheReverseOfTheTurn(t *testing.T) {
	const w, h = 8, 4
	for orientation := 1; orientation <= 8; orientation++ {
		src := image.NewRGBA(image.Rect(0, 0, w, h))
		// One marked pixel of the stored picture.
		const sx, sy = 6, 1
		src.SetRGBA(sx, sy, color.RGBA{R: 255, A: 255})
		turned := orient(src, orientation)
		tw, th := turned.Bounds().Dx(), turned.Bounds().Dy()
		found := false
		for y := range th {
			for x := range tw {
				if turned.RGBAAt(x, y).R != 255 {
					continue
				}
				found = true
				// The middle of the pixel, as a part of the picture.
				gx, gy := StoredPoint((float64(x)+0.5)/float64(tw), (float64(y)+0.5)/float64(th), orientation)
				if int(gx*w) != sx || int(gy*h) != sy {
					t.Errorf("orientation %d: the pixel at %d, %d goes back to %.2f, %.2f", orientation, x, y, gx*w, gy*h)
				}
			}
		}
		if !found {
			t.Errorf("orientation %d: the marked pixel is gone", orientation)
		}
	}
}
