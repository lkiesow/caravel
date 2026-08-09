// Package imaging decodes uploaded images and caps their dimensions before
// storage, using only stdlib + golang.org/x/image (no CGO/libvips), per plan
// Section 3.4.
package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const MaxDimension = 2000

type Result struct {
	Data        []byte
	ContentType string
	Width       int
	Height      int
}

// DecodeAndResize reads an image (JPEG/PNG/GIF/WebP), and if either
// dimension exceeds MaxDimension, scales it down to fit. WebP inputs are
// always re-encoded as JPEG since golang.org/x/image/webp only supports
// decoding, not encoding. Other formats are re-encoded in their original
// format so simple images (icons, screenshots) don't balloon in size or
// lose transparency.
func DecodeAndResize(r io.Reader) (Result, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Result{}, err
	}

	format, img, err := decode(data)
	if err != nil {
		return Result{}, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > MaxDimension || h > MaxDimension {
		img = resize(img, w, h)
	}

	return encode(img, format)
}

func decode(data []byte) (format string, img image.Image, err error) {
	format, img, err = decodeStdlib(data)
	if err == nil {
		return format, img, nil
	}
	if webpImg, webpErr := webp.Decode(bytes.NewReader(data)); webpErr == nil {
		return "webp", webpImg, nil
	}
	return "", nil, fmt.Errorf("unsupported or corrupt image: %w", err)
}

func decodeStdlib(data []byte) (string, image.Image, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	return format, img, err
}

func resize(img image.Image, w, h int) image.Image {
	scale := float64(MaxDimension) / float64(w)
	if hScale := float64(MaxDimension) / float64(h); hScale < scale {
		scale = hScale
	}
	newW, newH := int(float64(w)*scale), int(float64(h)*scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

func encode(img image.Image, format string) (Result, error) {
	var buf bytes.Buffer
	var contentType string

	switch format {
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return Result{}, err
		}
		contentType = "image/png"
	case "gif":
		if err := gif.Encode(&buf, img, nil); err != nil {
			return Result{}, err
		}
		contentType = "image/gif"
	default: // jpeg, webp, and any other decodable-but-unencodable format
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return Result{}, err
		}
		contentType = "image/jpeg"
	}

	bounds := img.Bounds()
	return Result{Data: buf.Bytes(), ContentType: contentType, Width: bounds.Dx(), Height: bounds.Dy()}, nil
}
