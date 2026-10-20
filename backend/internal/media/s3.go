package media

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3 struct {
	client      *minio.Client
	bucket      string
	mu          sync.Mutex
	initialized bool
}

func NewS3(endpoint, bucket, accessKey, secretKey string) (*S3, error) {
	if endpoint == "" {
		return &S3{}, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, ErrInvalid
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: u.Scheme == "https", Region: "us-east-1", Transport: transport, MaxRetries: 2})
	if err != nil {
		return nil, ErrInvalid
	}
	return &S3{client: client, bucket: bucket}, nil
}
func (s *S3) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return ErrUnavailable
	}
	if s.initialized {
		return nil
	}
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return ErrUnavailable
	}
	if !exists {
		if err = s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			code := minio.ToErrorResponse(err).Code
			if code != "BucketAlreadyOwnedByYou" {
				return ErrUnavailable
			}
		}
	}
	policy, err := s.client.GetBucketPolicy(ctx, s.bucket)
	if err != nil && minio.ToErrorResponse(err).Code != "NoSuchBucketPolicy" {
		return ErrUnavailable
	}
	// A dedicated private bucket must have no anonymous/bucket policy. Never overwrite it.
	if strings.TrimSpace(policy) != "" {
		return ErrUnavailable
	}
	s.initialized = true
	return nil
}
func (s *S3) Put(ctx context.Context, key string, body []byte, mime string) error {
	if err := s.Init(ctx); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: mime, DisableMultipart: true})
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	if err := s.Init(ctx); err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer object.Close()
	body, err := io.ReadAll(io.LimitReader(object, MaxFileBytes+1))
	if err != nil || len(body) > MaxFileBytes {
		return nil, ErrUnavailable
	}
	return body, nil
}
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := s.Init(ctx); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return ErrUnavailable
	}
	return nil
}
