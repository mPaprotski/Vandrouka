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
	provider    string
	controlHTTP *http.Client
}

func NewS3(endpoint, bucket, accessKey, secretKey string) (*S3, error) {
	return NewObjectStorage("s3", endpoint, bucket, accessKey, secretKey)
}

func NewObjectStorage(provider, endpoint, bucket, accessKey, secretKey string) (*S3, error) {
	if provider != "s3" && provider != "r2" && provider != "b2" {
		return nil, ErrInvalid
	}
	if endpoint == "" {
		return &S3{}, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, ErrInvalid
	}
	if provider == "r2" && (u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".r2.cloudflarestorage.com")) {
		return nil, ErrInvalid
	}
	region := "us-east-1"
	if provider == "r2" {
		region = "auto"
	}
	lookup := minio.BucketLookupAuto
	if provider == "b2" {
		parts := strings.Split(u.Hostname(), ".")
		if u.Scheme != "https" || len(parts) != 4 || parts[0] != "s3" || parts[1] == "" || parts[2] != "backblazeb2" || parts[3] != "com" || (u.Port() != "" && u.Port() != "443") {
			return nil, ErrInvalid
		}
		region = parts[1]
		lookup = minio.BucketLookupPath
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: u.Scheme == "https", Region: region, Transport: transport, MaxRetries: 2, BucketLookup: lookup})
	if err != nil {
		return nil, ErrInvalid
	}
	controlHTTP := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &S3{client: client, bucket: bucket, provider: provider, controlHTTP: controlHTTP}, nil
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
	if !exists && (s.provider == "r2" || s.provider == "b2") {
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
	// R2 does not implement bucket policies. Public access must be disabled in its dashboard.
	if s.provider == "r2" {
		s.initialized = true
		return nil
	}
	if s.provider == "b2" {
		if err := s.checkB2Private(ctx); err != nil {
			return err
		}
		s.initialized = true
		return nil
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
	if s.provider == "b2" {
		return s.deleteB2Versions(ctx, key)
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return ErrUnavailable
	}
	return nil
}
