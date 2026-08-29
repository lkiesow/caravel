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

// MaxPixels caps the *source* dimensions, checked from the header before the
// image is decoded. MaxDimension cannot do this job: it is applied after
// decoding, by which point the full-size bitmap has already been allocated, so
// a few hundred KB of PNG declaring 30000x30000 would ask for gigabytes of
// RGBA before anything looked at its size. 100 megapixels is past the outer
// edge of consumer cameras and roughly 400MB of RGBA at peak.
const MaxPixels = 100_000_000

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
//
// A JPEG carrying an EXIF Orientation is turned the right way up first. The
// re-encode drops all EXIF -- which is what keeps camera GPS tags off our
// disk -- so the rotation has to be baked into the pixels or it is lost, and a
// portrait phone photo ends up displayed on its side.
func DecodeAndResize(r io.Reader) (Result, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Result{}, err
	}
	return DecodeAndResizeBytes(data)
}

// DecodeAndResizeBytes is DecodeAndResize for callers that already hold the
// bytes. Everything below here works on the slice anyway -- the decode and the
// EXIF read both want it -- so wrapping a slice in a reader only to read it
// straight back out costs a second copy of the whole image. At a 50MB limit
// that was 100MB in flight per concurrent upload before the bitmap.
func DecodeAndResizeBytes(data []byte) (Result, error) {
	format, img, err := decode(data)
	if err != nil {
		return Result{}, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > MaxDimension || h > MaxDimension {
		img = resize(img, w, h)
	}
	if format == "jpeg" {
		// After the downscale, not before: rotating is isometric, so it cannot
		// change whether the cap was exceeded, and this way the pixel loop runs
		// over at most MaxDimension rather than a full-size phone photo.
		img = applyOrientation(img, jpegOrientation(data))
	}

	return encode(img, format)
}

// decode reads data as an image, refusing anything whose declared dimensions
// exceed MaxPixels before it allocates the bitmap.
//
// A header we cannot read at all falls through to the WebP path, since that is
// how an unregistered format has always been handled here. A header we *can*
// read that is simply too big does not fall through: it is a decision about
// this image, not a failure to understand it.
func decode(data []byte) (format string, img image.Image, err error) {
	if cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(data)); cfgErr == nil {
		if pixErr := checkPixels(cfg.Width, cfg.Height); pixErr != nil {
			return "", nil, pixErr
		}
		format, img, err = decodeStdlib(data)
		if err == nil {
			return format, img, nil
		}
	} else {
		err = cfgErr
	}
	if cfg, cfgErr := webp.DecodeConfig(bytes.NewReader(data)); cfgErr == nil {
		if pixErr := checkPixels(cfg.Width, cfg.Height); pixErr != nil {
			return "", nil, pixErr
		}
		if webpImg, webpErr := webp.Decode(bytes.NewReader(data)); webpErr == nil {
			return "webp", webpImg, nil
		}
	}
	return "", nil, fmt.Errorf("unsupported or corrupt image: %w", err)
}

func checkPixels(w, h int) error {
	// Compared separately first so the product cannot overflow int on a 32-bit
	// build: a header can declare whatever it likes.
	if w > MaxPixels || h > MaxPixels || int64(w)*int64(h) > MaxPixels {
		return fmt.Errorf("image is %dx%d, over the maximum of %d pixels", w, h, MaxPixels)
	}
	return nil
}

func decodeStdlib(data []byte) (string, image.Image, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	return format, img, err
}

// applyOrientation turns img the way its EXIF Orientation says it should be
// displayed, so the pixels themselves carry the rotation once the metadata has
// been dropped by the re-encode. Orientation 1, and anything outside 1-8,
// returns img untouched, so the common case allocates nothing.
//
// Each value is written out as an inverse mapping -- where in the source a
// given destination pixel comes from -- because that is what the test asserts
// corner by corner. Values 5 to 8 transpose the axes, so the result swaps
// width and height.
func applyOrientation(img image.Image, orientation int) image.Image {
	if orientation <= orientationNormal || orientation > 8 {
		return img
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// source picks the source pixel for destination pixel (dx, dy), both in
	// zero-based coordinates.
	var source func(dx, dy int) (int, int)
	dstW, dstH := w, h
	switch orientation {
	case 2: // mirrored horizontally
		source = func(dx, dy int) (int, int) { return w - 1 - dx, dy }
	case 3: // rotated 180
		source = func(dx, dy int) (int, int) { return w - 1 - dx, h - 1 - dy }
	case 4: // mirrored vertically
		source = func(dx, dy int) (int, int) { return dx, h - 1 - dy }
	case 5: // transposed along the main diagonal
		dstW, dstH = h, w
		source = func(dx, dy int) (int, int) { return dy, dx }
	case 6: // rotated 90 clockwise -- the usual portrait phone photo
		dstW, dstH = h, w
		source = func(dx, dy int) (int, int) { return dy, h - 1 - dx }
	case 7: // transposed along the anti-diagonal
		dstW, dstH = h, w
		source = func(dx, dy int) (int, int) { return w - 1 - dy, h - 1 - dx }
	case 8: // rotated 90 counter-clockwise
		dstW, dstH = h, w
		source = func(dx, dy int) (int, int) { return w - 1 - dy, dx }
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := 0; dy < dstH; dy++ {
		for dx := 0; dx < dstW; dx++ {
			sx, sy := source(dx, dy)
			dst.Set(dx, dy, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
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
