package media

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"io"
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
	Tombstone(context.Context, int64, string) (string, error)
	Purge(context.Context, string, func(context.Context, string) error) error
	Cleanup(context.Context, time.Time, time.Time, func(context.Context, string) error) error
}
type Service struct {
	Repository Repository
	Objects    Objects
	Now        func() time.Time
}

func New(repository Repository, objects Objects) *Service {
	return &Service{Repository: repository, Objects: objects, Now: time.Now}
}
func (s *Service) Upload(ctx context.Context, owner, place int64, reader io.Reader, mime string) (visits.Photo, error) {
	if owner < 1 || place < 1 {
		return visits.Photo{}, ErrInvalid
	}
	cleaned, err := Sanitize(reader, mime)
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
	ready, err := s.Repository.Complete(ctx, owner, photo.ID, cleaned, func(ctx context.Context, key string) error {
		return s.Objects.Put(ctx, key, cleaned.Body, cleaned.MIME)
	})
	if err != nil {
		// A separate short context persists recovery even if the upload was cancelled.
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Repository.Abort(cleanup, owner, photo.ID)
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
	if !validID(id) {
		return ErrNotFound
	}
	key, err := s.Repository.Tombstone(ctx, owner, id)
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
