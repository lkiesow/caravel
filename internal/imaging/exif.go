package imaging

import "encoding/binary"

// orientationNormal is EXIF Orientation 1: the stored pixels are already the
// way up the picture should be displayed. It is also what every failure path
// below returns, so unreadable metadata simply means no rotation.
const orientationNormal = 1

// tagOrientation is the EXIF/TIFF tag number for Orientation.
const tagOrientation = 0x0112

// jpegOrientation returns the EXIF Orientation value (1-8) recorded in a JPEG,
// or 1 when the file carries none or its metadata is unreadable.
//
// Phone cameras write the sensor read-out unrotated and record how to turn it
// here, so this is what tells a portrait photo apart from a landscape one.
// Only enough of the format is parsed to reach that one tag: the JPEG marker
// segments, then the TIFF header and IFD0 inside the APP1 Exif payload.
//
// It never reports an error. A truncated or malformed APP1 segment must not
// stop an otherwise decodable photo from being uploaded.
func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 { // SOI
		return orientationNormal
	}

	for i := 2; i+3 < len(data); {
		if data[i] != 0xFF {
			return orientationNormal // not at a marker boundary; give up
		}
		marker := data[i+1]
		switch {
		case marker == 0xFF:
			i++ // fill byte, the next byte is the real marker
			continue
		case marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7):
			i += 2 // standalone marker, no payload
			continue
		case marker == 0xDA || marker == 0xD9:
			return orientationNormal // start of scan / end of image: no metadata past here
		}

		// Every other marker carries a big-endian length that includes the
		// two length bytes themselves.
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if length < 2 || i+2+length > len(data) {
			return orientationNormal
		}
		payload := data[i+4 : i+2+length]
		if marker == 0xE1 && len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
			return tiffOrientation(payload[6:])
		}
		i += 2 + length
	}
	return orientationNormal
}

// tiffOrientation parses the TIFF block inside an APP1 Exif payload and
// returns the Orientation entry from IFD0. All offsets in the block are
// relative to the start of the block itself, not to the start of the file.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return orientationNormal
	}

	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return orientationNormal
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return orientationNormal
	}

	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return orientationNormal
	}
	count := int(order.Uint16(tiff[ifd : ifd+2]))
	if ifd+2+count*12 > len(tiff) {
		return orientationNormal
	}

	for n := 0; n < count; n++ {
		entry := tiff[ifd+2+n*12:]
		if order.Uint16(entry[:2]) != tagOrientation {
			continue
		}
		// Orientation is a SHORT, so its value lives inline in the first two
		// bytes of the entry's 4-byte value field rather than at an offset.
		if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return orientationNormal
		}
		value := int(order.Uint16(entry[8:10]))
		if value < 1 || value > 8 {
			return orientationNormal
		}
		return value
	}
	return orientationNormal
}
