// Package imaging turns a photo or a scan into a smaller JPEG. It uses the
// standard library only and reads JPEG, PNG and GIF files.
package imaging

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // GIF decoder
	"image/jpeg"
	_ "image/png" // PNG decoder
	"io"
)

// Errors that Render returns.
var (
	ErrUnsupported = errors.New("imaging: the file is not a JPEG, PNG or GIF image")
	ErrTooLarge    = errors.New("imaging: the image has too many pixels")
)

// Region is a part of an image, in percent of its width and height.
type Region struct {
	X1, Y1, X2, Y2 int
}

// Valid reports whether the region lies inside the image and is not empty.
func (r Region) Valid() bool {
	return r.X1 >= 0 && r.Y1 >= 0 && r.X2 <= 100 && r.Y2 <= 100 && r.X1 < r.X2 && r.Y1 < r.Y2
}

// Options control Render.
type Options struct {
	// MaxSide is the longest side of the result, in pixels.
	MaxSide int
	// Region selects a part of the image. Gramps defines it on the stored
	// pixels, before the camera orientation applies.
	Region *Region
	// MaxPixels limits the size of the source image.
	MaxPixels int64
	// MaxBytes limits the memory of the decoded source image. A picture with
	// 16 bits for each channel needs 8 bytes for each pixel.
	MaxBytes int64
	// Quality is the JPEG quality from 1 to 100.
	Quality int
}

// Orientation returns the way the camera held the picture: an EXIF value
// from 1 to 8. A picture without this mark has the value 1. A part of a
// picture that the tree stores is defined on the stored pixels, before this
// turn, so a program that shows the turned picture needs the value.
func Orientation(src io.ReadSeeker) int {
	_, format, err := image.DecodeConfig(src)
	if err != nil || format != "jpeg" {
		return 1
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return 1
	}
	return exifOrientation(src)
}

// decodeSource reads the picture and the way the camera held it.
func decodeSource(src io.ReadSeeker, opts Options) (image.Image, int, error) {
	config, format, err := image.DecodeConfig(src)
	if err != nil {
		return nil, 0, ErrUnsupported
	}
	if config.Width <= 0 || config.Height <= 0 {
		return nil, 0, ErrUnsupported
	}
	if !Fits(config, opts.MaxPixels, opts.MaxBytes) {
		return nil, 0, ErrTooLarge
	}
	orientation := 1
	if format == "jpeg" {
		if _, err := src.Seek(0, io.SeekStart); err != nil {
			return nil, 0, err
		}
		orientation = exifOrientation(src)
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	img, _, err := image.Decode(src)
	if err != nil {
		return nil, 0, ErrUnsupported
	}
	return img, orientation, nil
}

// Pixels reads an image and returns its pixels the way a person sees the
// picture, not larger than MaxSide, and the orientation of the stored file.
// The search for faces uses it. It ignores Region and Quality.
func Pixels(src io.ReadSeeker, opts Options) (*image.RGBA, int, error) {
	img, orientation, err := decodeSource(src, opts)
	if err != nil {
		return nil, 0, err
	}
	crop := img.Bounds()
	width, height := crop.Dx(), crop.Dy()
	if longest := max(width, height); opts.MaxSide > 0 && longest > opts.MaxSide {
		width = max(1, width*opts.MaxSide/longest)
		height = max(1, height*opts.MaxSide/longest)
	}
	return orient(shrink(img, crop, width, height), orientation), orientation, nil
}

// Render reads an image and returns a JPEG that fits into MaxSide. It never
// makes an image larger.
func Render(src io.ReadSeeker, opts Options) ([]byte, error) {
	img, orientation, err := decodeSource(src, opts)
	if err != nil {
		return nil, err
	}
	crop := img.Bounds()
	if opts.Region != nil && opts.Region.Valid() {
		w, h := crop.Dx(), crop.Dy()
		part := image.Rect(
			crop.Min.X+w*opts.Region.X1/100, crop.Min.Y+h*opts.Region.Y1/100,
			crop.Min.X+w*opts.Region.X2/100, crop.Min.Y+h*opts.Region.Y2/100,
		)
		if part.Dx() > 0 && part.Dy() > 0 {
			crop = part
		}
	}
	width, height := crop.Dx(), crop.Dy()
	if longest := max(width, height); opts.MaxSide > 0 && longest > opts.MaxSide {
		width = max(1, width*opts.MaxSide/longest)
		height = max(1, height*opts.MaxSide/longest)
	}
	small := shrink(img, crop, width, height)
	quality := opts.Quality
	if quality <= 0 || quality > 100 {
		quality = 82
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, orient(small, orientation), &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// bytesPerPixel returns the memory that the decoder uses for one pixel.
func bytesPerPixel(model color.Model) int64 {
	switch model {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2
	case color.YCbCrModel:
		return 3
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	}
	if _, ok := model.(color.Palette); ok {
		return 1
	}
	return 4
}

// Fits reports whether a picture stays inside the limits of pixels and of
// decoded bytes. A limit of 0 means no limit.
func Fits(config image.Config, maxPixels, maxBytes int64) bool {
	pixels := int64(config.Width) * int64(config.Height)
	if config.Width <= 0 || config.Height <= 0 || (maxPixels > 0 && pixels > maxPixels) {
		return false
	}
	return maxBytes <= 0 || pixels*bytesPerPixel(config.ColorModel) <= maxBytes
}

// shrink scales a part of an image down by averaging the source pixels of
// each result pixel. It works in bands, so it never holds a full copy of a
// large source. A transparent area becomes white.
func shrink(src image.Image, crop image.Rectangle, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	sw, sh := crop.Dx(), crop.Dy()
	white := image.NewUniform(color.White)
	var band *image.RGBA
	for y := 0; y < height; y++ {
		y0 := y * sh / height
		y1 := max((y+1)*sh/height, y0+1)
		rows := y1 - y0
		if band == nil || band.Bounds().Dy() < rows {
			band = image.NewRGBA(image.Rect(0, 0, sw, rows))
		}
		area := image.Rect(0, 0, sw, rows)
		draw.Draw(band, area, white, image.Point{}, draw.Src)
		draw.Draw(band, area, src, image.Pt(crop.Min.X, crop.Min.Y+y0), draw.Over)
		for x := 0; x < width; x++ {
			x0 := x * sw / width
			x1 := max((x+1)*sw/width, x0+1)
			var r, g, b uint32
			for row := 0; row < rows; row++ {
				line := band.Pix[row*band.Stride+x0*4 : row*band.Stride+x1*4]
				for i := 0; i+3 < len(line); i += 4 {
					r += uint32(line[i])
					g += uint32(line[i+1])
					b += uint32(line[i+2])
				}
			}
			count := uint32(rows * (x1 - x0)) // #nosec G115 -- both factors are small and positive.
			offset := y*dst.Stride + x*4
			dst.Pix[offset] = uint8(r / count)   // #nosec G115 -- an average of bytes is a byte.
			dst.Pix[offset+1] = uint8(g / count) // #nosec G115
			dst.Pix[offset+2] = uint8(b / count) // #nosec G115
			dst.Pix[offset+3] = 255
		}
	}
	return dst
}

// orient turns the image the way the camera says. The values 1 to 8 are the
// EXIF orientations.
func orient(src *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	swap := orientation >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			from := y*src.Stride + x*4
			to := dy*dst.Stride + dx*4
			copy(dst.Pix[to:to+4], src.Pix[from:from+4])
		}
	}
	return dst
}

// StoredPoint takes a place on the picture as a person sees it and returns
// the same place on the stored pixels. Both places are parts of the width
// and of the height, from 0 to 1. It is the reverse of the turn that orient does.
func StoredPoint(x, y float64, orientation int) (float64, float64) {
	switch orientation {
	case 2:
		return 1 - x, y
	case 3:
		return 1 - x, 1 - y
	case 4:
		return x, 1 - y
	case 5:
		return y, x
	case 6:
		return y, 1 - x
	case 7:
		return 1 - y, 1 - x
	case 8:
		return 1 - y, x
	}
	return x, y
}

// exifLimit is how much of a JPEG the orientation reader looks at.
const exifLimit = 256 << 10

// exifOrientation returns the EXIF orientation of a JPEG, or 1.
func exifOrientation(r io.Reader) int {
	data, err := io.ReadAll(io.LimitReader(r, exifLimit))
	if err != nil || len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	at := 2
	for at+4 <= len(data) {
		if data[at] != 0xFF {
			return 1
		}
		marker := data[at+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			at += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		size := int(data[at+2])<<8 | int(data[at+3])
		if size < 2 || at+2+size > len(data) {
			return 1
		}
		if marker == 0xE1 {
			if value := tiffOrientation(data[at+4 : at+2+size]); value != 0 {
				return value
			}
		}
		at += 2 + size
	}
	return 1
}

func tiffOrientation(segment []byte) int {
	if len(segment) < 14 || string(segment[:6]) != "Exif\x00\x00" {
		return 0
	}
	tiff := segment[6:]
	var u16 func(b []byte) int
	var u32 func(b []byte) int
	switch string(tiff[:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
		u32 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	case "MM":
		u16 = func(b []byte) int { return int(b[1]) | int(b[0])<<8 }
		u32 = func(b []byte) int { return int(b[3]) | int(b[2])<<8 | int(b[1])<<16 | int(b[0])<<24 }
	default:
		return 0
	}
	if u16(tiff[2:4]) != 42 {
		return 0
	}
	offset := u32(tiff[4:8])
	if offset < 8 || offset+2 > len(tiff) {
		return 0
	}
	entries := u16(tiff[offset : offset+2])
	for i := 0; i < entries; i++ {
		start := offset + 2 + i*12
		if start+12 > len(tiff) {
			return 0
		}
		if u16(tiff[start:start+2]) == 0x0112 {
			value := u16(tiff[start+8 : start+10])
			if value >= 1 && value <= 8 {
				return value
			}
			return 0
		}
	}
	return 0
}
