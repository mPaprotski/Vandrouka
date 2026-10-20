package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func fixtureImage(t *testing.T, format string) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	im.Set(0, 0, color.NRGBA{R: 255, A: 255})
	im.Set(1, 2, color.NRGBA{G: 255, A: 255})
	var b bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&b, im, nil)
	case "png":
		err = png.Encode(&b, im)
	case "gif":
		err = gif.Encode(&b, im, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func withExif(raw []byte) []byte {
	// Little-endian TIFF: Orientation=6 (90 degrees clockwise).
	exif := []byte("Exif\x00\x00II\x2a\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00\x00\x00\x00")
	exif = append(exif, []byte("private GPS metadata")...)
	output := []byte{0xff, 0xd8, 0xff, 0xe1}
	output = binary.BigEndian.AppendUint16(output, uint16(len(exif)+2))
	output = append(output, exif...)
	return append(output, raw[2:]...)
}
func pngChunk(kind string, body []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(chunk, kind...)
	chunk = append(chunk, body...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}
func TestSanitize(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		input := fixtureImage(t, format)
		if format == "jpeg" {
			input = withExif(input)
		} else {
			input = append(append(append([]byte{}, input[:33]...), pngChunk("tEXt", []byte("GPS\x00private GPS metadata"))...), input[33:]...)
		}
		cleaned, err := Sanitize(bytes.NewReader(input), "image/"+format)
		if err != nil {
			t.Fatal(format, err)
		}
		if bytes.Contains(cleaned.Body, []byte("Exif")) || bytes.Contains(cleaned.Body, []byte("private GPS metadata")) {
			t.Fatal("metadata retained")
		}
		w, h := 2, 3
		if format == "jpeg" {
			w, h = 3, 2
		}
		if cleaned.Width != w || cleaned.Height != h || cleaned.MIME != "image/"+format || len(cleaned.SHA256) != 32 {
			t.Fatal("geometry/orientation or metadata", cleaned.Width, cleaned.Height)
		}
		if _, _, err := image.Decode(bytes.NewReader(cleaned.Body)); err != nil {
			t.Fatal("invalid output", err)
		}
	}
}
func TestSanitizeRejectsUnsafeInput(t *testing.T) {
	for _, entry := range []struct {
		input []byte
		mime  string
		want  error
	}{
		{fixtureImage(t, "png"), "image/jpeg", ErrUnsupported},
		{fixtureImage(t, "gif"), "image/png", ErrUnsupported},
		{fixtureImage(t, "jpeg"), "text/html", ErrUnsupported},
		{[]byte("<html>not an image</html>"), "image/jpeg", ErrInvalid},
		{fixtureImage(t, "jpeg")[:40], "image/jpeg", ErrInvalid},
	} {
		if _, err := Sanitize(bytes.NewReader(entry.input), entry.mime); !errors.Is(err, entry.want) {
			t.Fatalf("want %v got %v", entry.want, err)
		}
	}
	if _, err := Sanitize(strings.NewReader(strings.Repeat("x", MaxFileBytes+1)), "image/jpeg"); !errors.Is(err, ErrTooLarge) {
		t.Fatal("byte limit", err)
	}
	for _, dims := range [][2]uint32{{9000, 2}, {5000, 5000}} {
		raw := fixtureImage(t, "png")
		header := append([]byte{}, raw[16:29]...)
		binary.BigEndian.PutUint32(header[:4], dims[0])
		binary.BigEndian.PutUint32(header[4:8], dims[1])
		changed := append(append([]byte{}, raw[:8]...), pngChunk("IHDR", header)...)
		changed = append(changed, raw[33:]...)
		if _, err := Sanitize(bytes.NewReader(changed), "image/png"); !errors.Is(err, ErrTooLarge) {
			t.Fatal("pixel limit", dims, err)
		}
	}
}
