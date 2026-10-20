package media

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestS3Configuration(t *testing.T) {
	for _, endpoint := range []string{"file:///tmp/photos", "http://user:secret@host", "http://host/path", "http://host?secret=1"} {
		if _, err := NewS3(endpoint, "bucket", "key", "secret"); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
	disabled, err := NewS3("", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = disabled.Init(context.Background()); err != ErrUnavailable {
		t.Fatal("disabled storage exposed", err)
	}
}
func TestS3PrivateIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_MEDIA_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_MEDIA_S3_ENDPOINT not set")
	}
	bucket := fmt.Sprintf("vandrouka-media-test-%d", time.Now().UnixNano())
	objects, err := NewS3(endpoint, bucket, os.Getenv("TEST_MEDIA_S3_ACCESS_KEY"), os.Getenv("TEST_MEDIA_S3_SECRET_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = objects.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer objects.client.RemoveBucket(context.Background(), bucket)
	key := "private/test.jpg"
	defer objects.Delete(context.Background(), key)
	data := fixtureImage(t, "jpeg")
	if err = objects.Put(ctx, key, data, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	actual, err := objects.Get(ctx, key)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("S3 round trip", err)
	}
	for _, path := range []string{"/" + bucket, "/" + bucket + "/" + key} {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal("anonymous access not denied", response.StatusCode)
		}
	}
	// Refuse an existing bucket with a public policy instead of silently using it.
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
	if err = objects.client.SetBucketPolicy(ctx, bucket, policy); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewS3(endpoint, bucket, os.Getenv("TEST_MEDIA_S3_ACCESS_KEY"), os.Getenv("TEST_MEDIA_S3_SECRET_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	refused := fresh.Init(ctx)
	if err = objects.client.SetBucketPolicy(ctx, bucket, ""); err != nil {
		t.Fatal(err)
	}
	if refused != ErrUnavailable {
		t.Fatal("public bucket accepted", refused)
	}
	if err = objects.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = objects.Delete(ctx, key); err != nil {
		t.Fatal("deletion not idempotent", err)
	}
}
