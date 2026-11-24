package media

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/yourname/vandrouka/backend/internal/visits"
)

type Objects interface {
	Init(context.Context) error
	Put(context.Context, string, []byte, string) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
type Repository interface {
	Create(context.Context, int64, int64, string) (visits.Photo, error)
	Complete(context.Context, int64, string, Image, func(context.Context, string) error) (visits.Photo, error)
	Abort(context.Context, int64, string) error
	Read(context.Context, int64, string) (visits.Photo, error)
	Tombstone(context.Context, int64, string, bool) (string, error)
	Purge(context.Context, string, func(context.Context, string) error) error
	Cleanup(context.Context, time.Time, time.Time, func(context.Context, string) error) error
}
type Service struct {
	Repository Repository
	Anonymizer Anonymizer
	Objects    Objects
	Now        func() time.Time
	preparing  sync.Mutex
}

func New(repository Repository, objects Objects) *Service {
	return &Service{Repository: repository, Objects: objects, Now: time.Now}
}
func (s *Service) Upload(ctx context.Context, owner, place int64, reader io.Reader, mime string) (visits.Photo, error) {
	if owner < 1 || place < 1 {
		return visits.Photo{}, ErrInvalid
	}
	cleaned, err := s.prepare(ctx, reader, mime)
	if err != nil {
		return visits.Photo{}, err
	}
	if err = s.Objects.Init(ctx); err != nil {
		return visits.Photo{}, ErrUnavailable
	}
	photo, err := s.Repository.Create(ctx, owner, place, cleaned.MIME)
	if err != nil {
		return visits.Photo{}, err
	}
	return s.store(ctx, owner, photo.ID, cleaned)
}

// PrepareQuest requires an anonymizer before accepting any quest photo.
func (s *Service) PrepareQuest(ctx context.Context, reader io.Reader, mime string) (Image, error) {
	if s.Anonymizer == nil {
		return Image{}, ErrAnonymizerUnavailable
	}
	return s.prepare(ctx, reader, mime)
}

func (s *Service) prepare(ctx context.Context, reader io.Reader, mime string) (Image, error) {
	anonymize := s.Anonymizer != nil
	// Decode and encode compete for the same CPU as Deface. Reserve the shared
	// slot before decoding so a competing upload cannot slow down the active one.
	if anonymize {
		if !s.preparing.TryLock() {
			return Image{}, ErrAnonymizerBusy
		}
		defer s.preparing.Unlock()
	}
	cleaned, err := sanitize(reader, mime, anonymize)
	if err != nil {
		return Image{}, err
	}
	if anonymize {
		cleaned, err = s.Anonymizer.Anonymize(ctx, cleaned)
		if errors.Is(err, ErrAnonymizerBusy) {
			return Image{}, err
		}
		if err != nil {
			slog.WarnContext(ctx, "photo anonymization failed", "timed_out", errors.Is(err, context.DeadlineExceeded))
			return Image{}, ErrAnonymizerUnavailable
		}
	}
	return cleaned, nil
}

// SaveQuest writes an image prepared by PrepareQuest into an existing upload reservation.
func (s *Service) SaveQuest(ctx context.Context, owner int64, id string, prepared Image) error {
	if err := s.Objects.Init(ctx); err != nil {
		return ErrUnavailable
	}
	_, err := s.Repository.Complete(ctx, owner, id, prepared, func(ctx context.Context, key string) error {
		return s.Objects.Put(ctx, key, prepared.Body, prepared.MIME)
	})
	return err
}

func (s *Service) store(ctx context.Context, owner int64, id string, cleaned Image) (visits.Photo, error) {
	ready, err := s.Repository.Complete(ctx, owner, id, cleaned, func(ctx context.Context, key string) error {
		return s.Objects.Put(ctx, key, cleaned.Body, cleaned.MIME)
	})
	if err != nil {
		// A separate short context persists recovery even if the upload was cancelled.
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Repository.Abort(cleanup, owner, id)
		return visits.Photo{}, ErrUnavailable
	}
	return ready, nil
}
func (s *Service) Read(ctx context.Context, owner int64, id string) (visits.Photo, []byte, error) {
	if !validID(id) {
		return visits.Photo{}, nil, ErrNotFound
	}
	photo, err := s.Repository.Read(ctx, owner, id)
	if err != nil {
		return visits.Photo{}, nil, err
	}
	return s.ReadAuthorized(ctx, photo)
}

// ReadAuthorized reads bytes only after the caller has checked access to the metadata.
func (s *Service) ReadAuthorized(ctx context.Context, photo visits.Photo) (visits.Photo, []byte, error) {
	body, err := s.Objects.Get(ctx, photo.ObjectKey)
	if err != nil {
		return visits.Photo{}, nil, ErrUnavailable
	}
	sum := sha256.Sum256(body)
	if photo.SizeBytes == nil || int64(len(body)) != *photo.SizeBytes || subtle.ConstantTimeCompare(sum[:], photo.SHA256) != 1 {
		return visits.Photo{}, nil, ErrUnavailable
	}
	return photo, body, nil
}
func (s *Service) Delete(ctx context.Context, owner int64, id string) error {
	return s.remove(ctx, owner, id, false)
}

// Discard cleans a draft without deleting media that has concurrently been confirmed.
func (s *Service) Discard(ctx context.Context, owner int64, id string) error {
	return s.remove(ctx, owner, id, true)
}
func (s *Service) remove(ctx context.Context, owner int64, id string, unconfirmedOnly bool) error {
	if !validID(id) {
		return ErrNotFound
	}
	key, err := s.Repository.Tombstone(ctx, owner, id, unconfirmedOnly)
	if err != nil {
		return err
	}
	// Tombstone is committed before external IO; a durable deletion job retries failures.
	return s.Repository.Purge(ctx, key, s.Objects.Delete)
}
func (s *Service) Cleanup(ctx context.Context) error {
	now := s.Now().UTC()
	return s.Repository.Cleanup(ctx, now, now.Add(-24*time.Hour), s.Objects.Delete)
}
