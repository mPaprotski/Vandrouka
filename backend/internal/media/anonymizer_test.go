package media

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type failedAnonymizer struct{ err error }

func (a failedAnonymizer) Anonymize(context.Context, Image) (Image, error) {
	return Image{}, a.err
}

func TestUploadStopsBeforeStorageWhenAnonymizationFails(t *testing.T) {
	for _, tt := range []struct {
		name  string
		cause error
		want  error
	}{
		{"processing failure", errors.New("processor failed"), ErrAnonymizerUnavailable},
		{"processor busy", ErrAnonymizerBusy, ErrAnonymizerBusy},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// No storage dependencies are present. Reaching storage would panic.
			service := &Service{Anonymizer: failedAnonymizer{err: tt.cause}}
			photo := fixtureImage(t, "jpeg")
			_, err := service.Upload(context.Background(), 1, 1, bytes.NewReader(photo), "image/jpeg")
			if !errors.Is(err, tt.want) {
				t.Fatalf("Upload returned %v, want %v", err, tt.want)
			}
		})
	}
}

type heldAnonymizer struct {
	entered chan struct{}
	release chan struct{}
}

func (a heldAnonymizer) Anonymize(ctx context.Context, _ Image) (Image, error) {
	close(a.entered)
	select {
	case <-a.release:
		return Image{}, errors.New("stop before writing to storage")
	case <-ctx.Done():
		return Image{}, ctx.Err()
	}
}

type unreadablePhoto struct{ read bool }

func (p *unreadablePhoto) Read([]byte) (int, error) {
	p.read = true
	return 0, errors.New("competing upload must not read the photo")
}

func TestAnonymizationLimitsConcurrentImageDecoding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	held := heldAnonymizer{entered: make(chan struct{}), release: make(chan struct{})}
	service := &Service{Anonymizer: held}
	first := fixtureImage(t, "jpeg")
	finished := make(chan error, 1)
	go func() {
		_, err := service.Upload(ctx, 1, 1, bytes.NewReader(first), "image/jpeg")
		finished <- err
	}()
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal("upload did not reach the anonymizer")
	}
	racing := &unreadablePhoto{}
	if _, err := service.PrepareQuest(ctx, racing, "image/jpeg"); !errors.Is(err, ErrAnonymizerBusy) || racing.read {
		t.Fatalf("quest upload decoded a competing image: error=%v, read=%t", err, racing.read)
	}
	close(held.release)
	if err := <-finished; !errors.Is(err, ErrAnonymizerUnavailable) {
		t.Fatalf("first upload returned %v", err)
	}
}
