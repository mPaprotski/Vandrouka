package media

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"

	"github.com/disintegration/imaging"
)

const MaxFileBytes = 10 << 20
const MaxPixels = 20_000_000
const MaxDimension = 8192

var (
	ErrInvalid     = errors.New("invalid image request")
	ErrTooLarge    = errors.New("image too large")
	ErrUnsupported = errors.New("unsupported image format")
	ErrUnavailable = errors.New("media unavailable")
	ErrNotFound    = errors.New("photo not found")
	ErrConfirmed   = errors.New("photo is confirmed")
)

type Image struct {
	Body          []byte
	MIME          string
	Width, Height int
	SHA256        []byte
}
type cappedBuffer struct{ buffer bytes.Buffer }

func (b *cappedBuffer) Write(value []byte) (int, error) {
	if len(value) > MaxFileBytes-b.buffer.Len() {
		return 0, ErrTooLarge
	}
	return b.buffer.Write(value)
}
func (b *cappedBuffer) Bytes() []byte { return b.buffer.Bytes() }
func Sanitize(reader io.Reader, declaredMIME string) (Image, error) {
	if declaredMIME != "image/jpeg" && declaredMIME != "image/png" {
		return Image{}, ErrUnsupported
	}
	raw, err := io.ReadAll(io.LimitReader(reader, MaxFileBytes+1))
	if err != nil {
		return Image{}, ErrInvalid
	}
	if len(raw) > MaxFileBytes {
		return Image{}, ErrTooLarge
	}
	// Do not dispatch untrusted TIFF/BMP/etc to decoders registered by imaging.
	detected := http.DetectContentType(raw)
	if detected != "image/jpeg" && detected != "image/png" {
		if strings.HasPrefix(detected, "image/") {
			return Image{}, ErrUnsupported
		}
		return Image{}, ErrInvalid
	}
	if detected != declaredMIME {
		return Image{}, ErrUnsupported
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Image{}, ErrInvalid
	}
	actual := "image/" + format
	if format != "jpeg" && format != "png" {
		return Image{}, ErrUnsupported
	}
	if actual != declaredMIME {
		return Image{}, ErrUnsupported
	}
	if config.Width < 1 || config.Height < 1 {
		return Image{}, ErrInvalid
	}
	if config.Width > MaxDimension || config.Height > MaxDimension || int64(config.Width)*int64(config.Height) > MaxPixels {
		return Image{}, ErrTooLarge
	}
	decoded, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return Image{}, ErrInvalid
	}
	var output cappedBuffer
	if format == "jpeg" {
		err = jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&output, decoded)
	}
	if errors.Is(err, ErrTooLarge) {
		return Image{}, ErrTooLarge
	}
	if err != nil {
		return Image{}, ErrInvalid
	}
	sum := sha256.Sum256(output.Bytes())
	bounds := decoded.Bounds()
	return Image{Body: output.Bytes(), MIME: actual, Width: bounds.Dx(), Height: bounds.Dy(), SHA256: sum[:]}, nil
}
func validID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range strings.ToLower(id) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
