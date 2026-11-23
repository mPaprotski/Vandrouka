package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Deface processes one photo at a time to bound memory use.
type Deface struct {
	path   string
	script string
	slot   chan struct{}
}

func NewDeface() (*Deface, error) {
	script := os.Getenv("MEDIA_DEFACE_SCRIPT")
	if script == "" {
		return nil, errors.New("MEDIA_DEFACE_SCRIPT is required")
	}
	if _, err := os.Stat(script); err != nil {
		return nil, err
	}
	path, err := exec.LookPath("python3")
	if err != nil {
		return nil, err
	}
	return &Deface{path: path, script: script, slot: make(chan struct{}, 1)}, nil
}

func (d *Deface) Anonymize(ctx context.Context, input Image) (Image, error) {
	select {
	case d.slot <- struct{}{}:
		defer func() { <-d.slot }()
	default:
		return Image{}, ErrAnonymizerBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	dir, err := os.MkdirTemp("", "vandrouka-photo-")
	if err != nil {
		return Image{}, err
	}
	defer os.RemoveAll(dir)
	ext := ".jpg"
	if input.MIME == "image/png" {
		ext = ".png"
	}
	source, output := filepath.Join(dir, "input"+ext), filepath.Join(dir, "output"+ext)
	if err := os.WriteFile(source, input.Body, 0600); err != nil {
		return Image{}, err
	}
	cmd := exec.CommandContext(ctx, d.path, d.script, source, output)
	cmd.Env = []string{"OPENBLAS_NUM_THREADS=1", "OMP_NUM_THREADS=1"}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Image{}, ctx.Err()
		}
		return Image{}, err
	}
	file, err := os.Open(output)
	if err != nil {
		return Image{}, err
	}
	defer file.Close()
	result, err := sanitize(file, input.MIME, true)
	if err != nil {
		return Image{}, err
	}
	if result.Width != input.Width || result.Height != input.Height {
		return Image{}, errors.New("deface changed image dimensions")
	}
	return result, nil
}

type Anonymizer interface {
	Anonymize(context.Context, Image) (Image, error)
}

var (
	ErrAnonymizerBusy        = errors.New("photo anonymizer busy")
	ErrAnonymizerUnavailable = errors.New("photo anonymizer unavailable")
)
