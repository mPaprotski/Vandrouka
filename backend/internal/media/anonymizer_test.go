package media

import (
	"bytes"
	"context"
	"errors"
	"testing"
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
