// Package vision holds the image processing the HUD reader is built on:
// colour matching, connected components, finding lines of text, and
// straightening them for glyph matching.
package vision

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

// Image is a 32-bit image as GDI captures it: B, G, R, A per pixel.
type Image struct {
	W, H, Stride int
	Pix          []byte
}

func NewImage(w, h int) *Image {
	return &Image{W: w, H: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
}

// At returns a pixel's colour.
func (im *Image) At(x, y int) Color {
	i := y*im.Stride + x*4
	return Color{im.Pix[i+2], im.Pix[i+1], im.Pix[i]}
}

// RGB returns a pixel's colour, 0-1 per channel.
func (im *Image) RGB(x, y int) (r, g, b float32) {
	i := y*im.Stride + x*4
	return float32(im.Pix[i+2]) / 255, float32(im.Pix[i+1]) / 255, float32(im.Pix[i]) / 255
}

// Crop copies r out of the image.
func (im *Image) Crop(r Rect) *Image {
	r = r.Clip(im.W, im.H)
	out := NewImage(r.W(), r.H())
	for y := range out.H {
		copy(out.Pix[y*out.Stride:(y+1)*out.Stride], im.Pix[(r.Y0+y)*im.Stride+r.X0*4:(r.Y0+y)*im.Stride+r.X1*4])
	}
	return out
}

// ToRGBA converts to a standard library image (for saving as PNG).
func (im *Image) ToRGBA() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, im.W, im.H))
	for y := range im.H {
		for x := range im.W {
			i, j := y*im.Stride+x*4, y*out.Stride+x*4
			out.Pix[j], out.Pix[j+1], out.Pix[j+2], out.Pix[j+3] = im.Pix[i+2], im.Pix[i+1], im.Pix[i], 255
		}
	}
	return out
}

// FromImage converts a standard library image.
func FromImage(img image.Image) *Image {
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	out := NewImage(b.Dx(), b.Dy())
	for i := range out.W * out.H {
		out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = rgba.Pix[i*4+2], rgba.Pix[i*4+1], rgba.Pix[i*4], 255
	}
	return out
}

// LoadImage reads a PNG, JPEG or BMP file (Elite's F10 screenshots are BMP).
func LoadImage(path string) (*Image, error) {
	if strings.EqualFold(filepath.Ext(path), ".bmp") {
		return loadBMP(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return FromImage(img), nil
}

// loadBMP reads uncompressed 24 and 32-bit BMP files.
func loadBMP(path string) (*Image, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 54 || b[0] != 'B' || b[1] != 'M' {
		return nil, errors.New("not a BMP file")
	}
	offset := int(binary.LittleEndian.Uint32(b[10:]))
	w := int(int32(binary.LittleEndian.Uint32(b[18:])))
	h := int(int32(binary.LittleEndian.Uint32(b[22:])))
	bpp := int(binary.LittleEndian.Uint16(b[28:]))
	compression := binary.LittleEndian.Uint32(b[30:])
	if bpp != 24 && bpp != 32 || compression != 0 && compression != 3 || w <= 0 || h == 0 {
		return nil, fmt.Errorf("unsupported BMP (%d bpp, compression %d)", bpp, compression)
	}
	topDown := h < 0
	if topDown {
		h = -h
	}
	bytesPerPixel := bpp / 8
	stride := (w*bytesPerPixel + 3) &^ 3
	if offset+stride*h > len(b) {
		return nil, errors.New("truncated BMP")
	}
	out := NewImage(w, h)
	for y := range h {
		src := h - 1 - y
		if topDown {
			src = y
		}
		row := b[offset+src*stride:]
		for x := range w {
			i := y*out.Stride + x*4
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = row[x*bytesPerPixel], row[x*bytesPerPixel+1], row[x*bytesPerPixel+2], 255
		}
	}
	return out, nil
}

// Rect is a pixel rectangle, [X0, X1) x [Y0, Y1).
type Rect struct{ X0, Y0, X1, Y1 int }

func (r Rect) W() int { return r.X1 - r.X0 }
func (r Rect) H() int { return r.Y1 - r.Y0 }

// Clip limits r to a w x h image.
func (r Rect) Clip(w, h int) Rect {
	return Rect{max(r.X0, 0), max(r.Y0, 0), min(r.X1, w), min(r.Y1, h)}
}

// Offset moves r by (dx, dy).
func (r Rect) Offset(dx, dy int) Rect { return Rect{r.X0 + dx, r.Y0 + dy, r.X1 + dx, r.Y1 + dy} }

// Intersect is the part of r inside o.
func (r Rect) Intersect(o Rect) Rect {
	return Rect{max(r.X0, o.X0), max(r.Y0, o.Y0), min(r.X1, o.X1), min(r.Y1, o.Y1)}
}

// Grid is a float image, usually a colour score per pixel.
type Grid struct {
	W, H int
	V    []float32
}

func NewGrid(w, h int) Grid { return Grid{W: w, H: h, V: make([]float32, w*h)} }

func (g Grid) At(x, y int) float32 { return g.V[y*g.W+x] }

// Sub copies r out of the grid.
func (g Grid) Sub(r Rect) Grid {
	r = r.Clip(g.W, g.H)
	if r.W() <= 0 || r.H() <= 0 {
		return Grid{}
	}
	out := NewGrid(r.W(), r.H())
	for y := range out.H {
		copy(out.V[y*out.W:(y+1)*out.W], g.V[(r.Y0+y)*g.W+r.X0:(r.Y0+y)*g.W+r.X1])
	}
	return out
}
