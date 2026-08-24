package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// The four corner colours of the test source image. They are far enough apart
// that JPEG quantisation cannot confuse one for another, which is what lets
// the assertions below name an exact corner after a rotation.
var (
	topLeft     = color.RGBA{255, 0, 0, 255}
	topRight    = color.RGBA{0, 255, 0, 255}
	bottomLeft  = color.RGBA{0, 0, 255, 255}
	bottomRight = color.RGBA{255, 255, 0, 255}
)

// sourceImage builds a deliberately asymmetric w x h image: each quadrant is a
// flat corner colour, so both the axis swap and the direction of a rotation
// are visible in the result. w and h should differ, and both be even.
func sourceImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c color.RGBA
			switch {
			case x < w/2 && y < h/2:
				c = topLeft
			case y < h/2:
				c = topRight
			case x < w/2:
				c = bottomLeft
			default:
				c = bottomRight
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// exifAPP1 builds a complete APP1 segment (marker, length and payload)
// declaring a single IFD0 entry: Orientation = value, big-endian.
func exifAPP1(orientation int) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("MM")                                        // big-endian
	binary.Write(&tiff, binary.BigEndian, uint16(42))             // magic
	binary.Write(&tiff, binary.BigEndian, uint32(8))              // IFD0 begins right after this header
	binary.Write(&tiff, binary.BigEndian, uint16(1))              // one entry
	binary.Write(&tiff, binary.BigEndian, uint16(tagOrientation)) // tag
	binary.Write(&tiff, binary.BigEndian, uint16(3))              // type SHORT
	binary.Write(&tiff, binary.BigEndian, uint32(1))              // count
	binary.Write(&tiff, binary.BigEndian, uint16(orientation))    // value, inline
	binary.Write(&tiff, binary.BigEndian, uint16(0))              // padding of the 4-byte value field
	binary.Write(&tiff, binary.BigEndian, uint32(0))              // no next IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)

	segment := []byte{0xFF, 0xE1}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(payload)+2))
	return append(segment, payload...)
}

// jpegWithAPP1 encodes img as JPEG and splices segment in directly after the
// SOI marker, which is where a camera writes its Exif block.
func jpegWithAPP1(t *testing.T, img image.Image, segment []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encoding the test JPEG: %v", err)
	}
	raw := buf.Bytes()
	out := make([]byte, 0, len(raw)+len(segment))
	out = append(out, raw[:2]...) // SOI
	out = append(out, segment...)
	return append(out, raw[2:]...)
}

// nearestCorner names which of the four corner colours c is closest to, so a
// JPEG round trip does not have to be exact for the assertion to be meaningful.
func nearestCorner(t *testing.T, c color.Color) string {
	t.Helper()
	r, g, b, _ := c.RGBA()
	best, bestDist := "", 1<<62
	for name, want := range map[string]color.RGBA{
		"top-left": topLeft, "top-right": topRight,
		"bottom-left": bottomLeft, "bottom-right": bottomRight,
	} {
		wr, wg, wb, _ := want.RGBA()
		dr, dg, db := int(r)-int(wr), int(g)-int(wg), int(b)-int(wb)
		if d := dr*dr + dg*dg + db*db; d < bestDist {
			best, bestDist = name, d
		}
	}
	return best
}

// decodeResult decodes what DecodeAndResize produced, so the assertions can
// look at actual pixels rather than trusting the reported dimensions.
func decodeResult(t *testing.T, res Result) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("decoding the result: %v", err)
	}
	return img
}

// assertCorners checks the four corners of img, sampling a little way inside
// so JPEG ringing at the very edge cannot flip a comparison.
func assertCorners(t *testing.T, img image.Image, tl, tr, bl, br string) {
	t.Helper()
	b := img.Bounds()
	inset := 3
	corners := []struct {
		name string
		x, y int
		want string
	}{
		{"top-left", b.Min.X + inset, b.Min.Y + inset, tl},
		{"top-right", b.Max.X - 1 - inset, b.Min.Y + inset, tr},
		{"bottom-left", b.Min.X + inset, b.Max.Y - 1 - inset, bl},
		{"bottom-right", b.Max.X - 1 - inset, b.Max.Y - 1 - inset, br},
	}
	for _, c := range corners {
		if got := nearestCorner(t, img.At(c.x, c.y)); got != c.want {
			t.Errorf("%s of the result holds the source %s colour, want %s", c.name, got, c.want)
		}
	}
}

// TestDecodeAndResizeOrientation walks all eight EXIF Orientation values. The
// source is 40x20 with a named colour per quadrant; each case says what the
// result should measure and which source corner should land in each of its own
// four corners.
func TestDecodeAndResizeOrientation(t *testing.T) {
	const srcW, srcH = 40, 20
	src := sourceImage(srcW, srcH)

	cases := []struct {
		orientation    int
		name           string
		wantW, wantH   int
		tl, tr, bl, br string
	}{
		{1, "normal", srcW, srcH,
			"top-left", "top-right", "bottom-left", "bottom-right"},
		{2, "mirrored horizontally", srcW, srcH,
			"top-right", "top-left", "bottom-right", "bottom-left"},
		{3, "rotated 180", srcW, srcH,
			"bottom-right", "bottom-left", "top-right", "top-left"},
		{4, "mirrored vertically", srcW, srcH,
			"bottom-left", "bottom-right", "top-left", "top-right"},
		{5, "transposed", srcH, srcW,
			"top-left", "bottom-left", "top-right", "bottom-right"},
		{6, "rotated 90 clockwise", srcH, srcW,
			"bottom-left", "top-left", "bottom-right", "top-right"},
		{7, "transposed on the anti-diagonal", srcH, srcW,
			"bottom-right", "top-right", "bottom-left", "top-left"},
		{8, "rotated 90 counter-clockwise", srcH, srcW,
			"top-right", "bottom-right", "top-left", "bottom-left"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := jpegWithAPP1(t, src, exifAPP1(tc.orientation))

			res, err := DecodeAndResize(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("DecodeAndResize: %v", err)
			}
			if res.Width != tc.wantW || res.Height != tc.wantH {
				t.Errorf("result is %dx%d, want %dx%d", res.Width, res.Height, tc.wantW, tc.wantH)
			}

			img := decodeResult(t, res)
			if got := img.Bounds(); got.Dx() != tc.wantW || got.Dy() != tc.wantH {
				t.Errorf("stored pixels are %dx%d, want %dx%d", got.Dx(), got.Dy(), tc.wantW, tc.wantH)
			}
			assertCorners(t, img, tc.tl, tc.tr, tc.bl, tc.br)
		})
	}
}

// TestDecodeAndResizeNoOrientation covers the inputs that must come out
// exactly as they went in: a JPEG with no Exif block at all, one whose Exif
// block is unreadable, and a PNG (whose eXIf chunk we do not parse).
func TestDecodeAndResizeNoOrientation(t *testing.T) {
	const srcW, srcH = 40, 20
	src := sourceImage(srcW, srcH)

	var plainJPEG bytes.Buffer
	if err := jpeg.Encode(&plainJPEG, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var plainPNG bytes.Buffer
	if err := png.Encode(&plainPNG, src); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	// A well-formed APP1 header whose TIFF block stops mid-entry.
	truncated := exifAPP1(6)
	truncated = truncated[:len(truncated)-8]
	binary.BigEndian.PutUint16(truncated[2:4], uint16(len(truncated)-2))

	cases := []struct {
		name string
		data []byte
	}{
		{"no exif at all", plainJPEG.Bytes()},
		{"orientation 1", jpegWithAPP1(t, src, exifAPP1(1))},
		{"truncated exif", jpegWithAPP1(t, src, truncated)},
		{"garbage in the app1 payload", jpegWithAPP1(t, src,
			append([]byte{0xFF, 0xE1, 0x00, 0x0A}, []byte("Exif\x00\x00\xDE\xAD")...))},
		{"png", plainPNG.Bytes()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := DecodeAndResize(bytes.NewReader(tc.data))
			if err != nil {
				t.Fatalf("DecodeAndResize: %v", err)
			}
			if res.Width != srcW || res.Height != srcH {
				t.Errorf("result is %dx%d, want the source %dx%d", res.Width, res.Height, srcW, srcH)
			}
			assertCorners(t, decodeResult(t, res),
				"top-left", "top-right", "bottom-left", "bottom-right")
		})
	}
}

// TestDecodeAndResizeOrientationWithDownscale is the real phone-photo shape:
// a portrait picture over the dimension cap. Both the cap and the rotation
// have to apply, and the cap has to be judged on the picture as a whole rather
// than on whichever axis happens to be stored first.
func TestDecodeAndResizeOrientationWithDownscale(t *testing.T) {
	src := sourceImage(MaxDimension+400, MaxDimension/2)
	data := jpegWithAPP1(t, src, exifAPP1(6))

	res, err := DecodeAndResize(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAndResize: %v", err)
	}
	if res.Height != MaxDimension {
		t.Errorf("result is %dx%d, want the long side scaled down to %d and turned upright",
			res.Width, res.Height, MaxDimension)
	}
	if res.Width >= res.Height {
		t.Errorf("result is %dx%d, still landscape after an orientation-6 rotation", res.Width, res.Height)
	}
	assertCorners(t, decodeResult(t, res),
		"bottom-left", "top-left", "bottom-right", "top-right")
}

// TestDecodeAndResizeStripsExif pins the privacy property the re-encode gives
// us: the orientation is read on the way in, but no EXIF -- and so no camera
// GPS tag -- is ever written back out.
func TestDecodeAndResizeStripsExif(t *testing.T) {
	data := jpegWithAPP1(t, sourceImage(40, 20), exifAPP1(6))
	if !bytes.Contains(data, []byte("Exif\x00\x00")) {
		t.Fatal("the test input carries no Exif block, so this proves nothing")
	}

	res, err := DecodeAndResize(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAndResize: %v", err)
	}
	if bytes.Contains(res.Data, []byte("Exif\x00\x00")) {
		t.Error("the stored image still carries an Exif block")
	}
	if jpegOrientation(res.Data) != orientationNormal {
		t.Error("the stored image still declares an orientation; it would be applied twice")
	}
}

// TestJPEGOrientationMalformed feeds the parser input it cannot make sense of.
// All of it has to come back as orientation 1 rather than erroring, because an
// unreadable Exif block must not stop an otherwise fine photo being uploaded.
func TestJPEGOrientationMalformed(t *testing.T) {
	cases := map[string][]byte{
		"empty":               {},
		"one byte":            {0xFF},
		"soi only":            {0xFF, 0xD8},
		"not a jpeg":          []byte("hello there, not an image"),
		"zero segment length": {0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x00},
		"length past the end": {0xFF, 0xD8, 0xFF, 0xE1, 0x7F, 0xFF, 'E', 'x'},
		"lost the marker":     {0xFF, 0xD8, 0x00, 0x11, 0x22, 0x33},
		"exif header only":    {0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0x00, 0x00},
		"unknown byte order": append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10},
			[]byte("Exif\x00\x00XX\x00\x2A\x00\x00\x00\x08")...),
		"bad tiff magic": append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10},
			[]byte("Exif\x00\x00MM\x00\x17\x00\x00\x00\x08")...),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if got := jpegOrientation(data); got != orientationNormal {
				t.Errorf("jpegOrientation = %d, want %d", got, orientationNormal)
			}
		})
	}
}

// TestJPEGOrientationTruncated cuts a real Exif-bearing JPEG off at every
// length there is. Nothing may panic or run off the end of the slice, and the
// answer always has to be a value applyOrientation will accept. Note that a
// cut past the end of the APP1 segment leaves the orientation genuinely
// readable, so the assertion is the valid range rather than a fixed value.
func TestJPEGOrientationTruncated(t *testing.T) {
	full := jpegWithAPP1(t, sourceImage(8, 4), exifAPP1(6))
	for n := 0; n <= len(full); n++ {
		if got := jpegOrientation(full[:n]); got < 1 || got > 8 {
			t.Fatalf("jpegOrientation of the first %d bytes = %d, outside 1-8", n, got)
		}
	}
}

// TestJPEGOrientationLittleEndian checks the II byte order too -- most phones
// write it, and the assertions above all run through the MM builder.
func TestJPEGOrientationLittleEndian(t *testing.T) {
	var tiff bytes.Buffer
	tiff.WriteString("II")
	binary.Write(&tiff, binary.LittleEndian, uint16(42))
	binary.Write(&tiff, binary.LittleEndian, uint32(8))
	binary.Write(&tiff, binary.LittleEndian, uint16(1))
	binary.Write(&tiff, binary.LittleEndian, uint16(tagOrientation))
	binary.Write(&tiff, binary.LittleEndian, uint16(3))
	binary.Write(&tiff, binary.LittleEndian, uint32(1))
	binary.Write(&tiff, binary.LittleEndian, uint16(8))
	binary.Write(&tiff, binary.LittleEndian, uint16(0))
	binary.Write(&tiff, binary.LittleEndian, uint32(0))

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segment := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(payload)+2))
	segment = append(segment, payload...)

	data := jpegWithAPP1(t, sourceImage(8, 4), segment)
	if got := jpegOrientation(data); got != 8 {
		t.Errorf("jpegOrientation = %d, want 8", got)
	}
}

// TestJPEGOrientationSkipsEarlierSegments makes sure the marker walk steps
// over a JFIF APP0 rather than stopping at the first thing that is not Exif --
// cameras that write both put APP0 first.
func TestJPEGOrientationSkipsEarlierSegments(t *testing.T) {
	app0 := []byte{0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00,
		0x01, 0x02, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
	segment := append(app0, exifAPP1(6)...)

	data := jpegWithAPP1(t, sourceImage(8, 4), segment)
	if got := jpegOrientation(data); got != 6 {
		t.Errorf("jpegOrientation = %d, want 6", got)
	}
}
