package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const privateB2ACL = `<AccessControlPolicy xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>owner</ID></Owner><AccessControlList><Grant><Grantee><ID>owner</ID></Grantee><Permission>FULL_CONTROL</Permission></Grant></AccessControlList></AccessControlPolicy>`

func b2TestStorage(t *testing.T, handler http.HandlerFunc) *S3 {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:  credentials.NewStaticV4("test-key", "test-secret", ""),
		Secure: true,
		Region: "eu-central-003", BucketLookup: minio.BucketLookupPath,
		Transport: server.Client().Transport, MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &S3{client: client, bucket: "photos", provider: "b2", controlHTTP: server.Client()}
}

func TestB2Configuration(t *testing.T) {
	for _, endpoint := range []string{
		"http://s3.eu-central-003.backblazeb2.com",
		"https://example.com", "https://s3.eu-central-003.backblazeb2.com.evil.example",
		"https://photos.s3.eu-central-003.backblazeb2.com",
		"https://s3.eu-central-003.backblazeb2.com/photos",
		"https://s3.eu-central-003.backblazeb2.com:8080",
	} {
		if _, err := NewObjectStorage("b2", endpoint, "photos", "key", "secret"); err != ErrInvalid {
			t.Fatalf("accepted endpoint %q", endpoint)
		}
	}
	for _, region := range []string{"eu-central-003", "us-west-004"} {
		storage, err := NewObjectStorage("b2", "https://s3."+region+".backblazeb2.com", "photos", "key", "secret")
		if err != nil {
			t.Fatal(err)
		}
		signed, err := storage.client.Presign(context.Background(), http.MethodGet, "photos", "", time.Minute, url.Values{"acl": {""}})
		if err != nil || !strings.Contains(signed.Query().Get("X-Amz-Credential"), "/"+region+"/s3/aws4_request") || signed.Path != "/photos/" {
			t.Fatalf("wrong B2 signing region or bucket path for %s", region)
		}
	}
}

func TestB2PrivateBucket(t *testing.T) {
	public := strings.Replace(privateB2ACL, "</AccessControlList>", `<Grant><Grantee><URI>http://acs.amazonaws.com/groups/global/AllUsers</URI></Grantee><Permission>READ</Permission></Grant></AccessControlList>`, 1)
	for _, test := range []struct {
		name   string
		body   string
		status int
		ok     bool
	}{
		{"private", privateB2ACL, 200, true},
		{"public", public, 200, false},
		{"denied", "", 403, false},
		{"malformed", "<AccessControlPolicy>", 200, false},
		{"empty", "<AccessControlPolicy/>", 200, false},
		{"wrong owner", strings.Replace(privateB2ACL, "<Grantee><ID>owner", "<Grantee><ID>other", 1), 200, false},
		{"oversized", privateB2ACL + strings.Repeat(" ", 64*1024), 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := b2TestStorage(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(200)
					return
				}
				if r.Method != http.MethodGet || !r.URL.Query().Has("acl") {
					t.Errorf("unexpected B2 initialization request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				if !strings.Contains(r.URL.Query().Get("X-Amz-Credential"), "/eu-central-003/s3/aws4_request") {
					t.Error("bucket ACL request lacks regional signature")
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			err := storage.Init(context.Background())
			if test.ok && err != nil || !test.ok && err != ErrUnavailable {
				t.Fatalf("unexpected privacy result %v", err)
			}
			if storage.initialized != test.ok {
				t.Fatal("incorrect initialization state")
			}
		})
	}
}

func TestB2MissingBucket(t *testing.T) {
	storage := b2TestStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("unexpected request for missing bucket %s", r.Method)
		}
		w.Header().Set("X-Amz-Error-Code", "NoSuchBucket")
		w.WriteHeader(404)
	})
	if err := storage.Init(context.Background()); err != ErrUnavailable {
		t.Fatal("missing bucket accepted", err)
	}
}

func TestB2UploadReadAndVersionDeletion(t *testing.T) {
	const key = "private/test.jpg"
	data := []byte("private photo bytes")
	var mu sync.Mutex
	var uploaded []byte
	deleted := map[string]bool{}
	lists := 0
	storage := b2TestStorage(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		query := r.URL.Query()
		w.Header().Set("ETag", `"00000000000000000000000000000000"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		switch {
		case r.Method == http.MethodHead:
			w.WriteHeader(200)
		case r.Method == http.MethodGet && query.Has("acl"):
			fmt.Fprint(w, privateB2ACL)
		case r.Method == http.MethodPut && r.URL.Path == "/photos/"+key:
			var err error
			uploaded, err = io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			w.WriteHeader(200)
		case r.Method == http.MethodGet && query.Has("versions"):
			lists++
			if query.Get("prefix") != key {
				t.Error("version listing lost exact-key prefix")
			}
			if len(deleted) == 3 {
				fmt.Fprint(w, `<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`)
			} else if query.Get("key-marker") == "" {
				fmt.Fprintf(w, `<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextVersionIdMarker>hide</NextVersionIdMarker><Version><Key>%s</Key><VersionId>v1</VersionId></Version><DeleteMarker><Key>%s</Key><VersionId>hide</VersionId></DeleteMarker></ListVersionsResult>`, key, key, key)
			} else {
				if query.Get("version-id-marker") != "hide" {
					t.Error("version continuation marker lost")
				}
				fmt.Fprintf(w, `<ListVersionsResult><IsTruncated>false</IsTruncated><Version><Key>%s</Key><VersionId>v2</VersionId></Version><Version><Key>%s.other</Key><VersionId>unrelated</VersionId></Version></ListVersionsResult>`, key, key)
			}
		case r.Method == http.MethodDelete:
			version := query.Get("versionId")
			if lists < 2 || r.URL.Path != "/photos/"+key || version == "" || version == "unrelated" {
				t.Errorf("unsafe version deletion path=%s version=%s", r.URL.Path, version)
			}
			deleted[version] = true
			w.WriteHeader(204)
		case r.Method == http.MethodGet && r.URL.Path == "/photos/"+key:
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data)
		default:
			t.Errorf("unexpected object request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := storage.Put(ctx, key, data, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	actual, err := storage.Get(ctx, key)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("photo round trip failed", err)
	}
	if err := storage.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := storage.Delete(ctx, key); err != nil {
		t.Fatal("repeated deletion failed", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Equal(uploaded, data) || !reflect.DeepEqual(deleted, map[string]bool{"v1": true, "v2": true, "hide": true}) {
		t.Fatalf("upload or physical version deletion mismatch: bytes=%q deleted=%v", uploaded, deleted)
	}
}

func TestB2DeletionFailureAndRetry(t *testing.T) {
	for _, failure := range []string{"list denied", "missing version", "delete denied"} {
		t.Run(failure, func(t *testing.T) {
			var mu sync.Mutex
			failed := false
			deleted := map[string]bool{}
			storage := b2TestStorage(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet && r.URL.Query().Has("versions") {
					if failure == "list denied" && !failed {
						failed = true
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `<ListVersionsResult><IsTruncated>false</IsTruncated>`)
					for _, version := range []string{"v1", "v2"} {
						if deleted[version] {
							continue
						}
						if failure == "missing version" && !failed {
							fmt.Fprint(w, `<Version><Key>photo.jpg</Key></Version>`)
							failed = true
						} else {
							fmt.Fprintf(w, `<Version><Key>photo.jpg</Key><VersionId>%s</VersionId></Version>`, version)
						}
					}
					fmt.Fprint(w, `</ListVersionsResult>`)
					return
				}
				if r.Method != http.MethodDelete || r.URL.Query().Get("versionId") == "" {
					t.Errorf("unexpected deletion request %s", r.Method)
					w.WriteHeader(400)
					return
				}
				version := r.URL.Query().Get("versionId")
				if failure == "delete denied" && version == "v2" && !failed {
					failed = true
					w.WriteHeader(403)
					return
				}
				deleted[version] = true
				w.WriteHeader(204)
			})
			storage.initialized = true
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := storage.Delete(ctx, "photo.jpg"); err != ErrUnavailable {
				t.Fatalf("failed physical deletion reported success: %v", err)
			}
			if err := storage.Delete(ctx, "photo.jpg"); err != nil {
				t.Fatal("retry failed", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(deleted, map[string]bool{"v1": true, "v2": true}) {
				t.Fatal("retry left object versions behind")
			}
		})
	}
}

func TestB2PrivateIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_MEDIA_B2_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_MEDIA_B2_ENDPOINT not set")
	}
	storage, err := NewObjectStorage("b2", endpoint, os.Getenv("TEST_MEDIA_B2_BUCKET"),
		os.Getenv("TEST_MEDIA_B2_ACCESS_KEY"), os.Getenv("TEST_MEDIA_B2_SECRET_KEY"))
	if err != nil {
		t.Fatal("invalid B2 test configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := storage.Init(ctx); err != nil {
		t.Fatal("private B2 initialization failed", err)
	}
	key := "integration/" + uuid.NewString() + ".jpg"
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := storage.Delete(cleanupCtx, key); err != nil {
			t.Errorf("B2 test object cleanup failed: %v", err)
		}
	}()
	data := fixtureImage(t, "jpeg")
	// Retry the same key to create multiple B2 versions and exercise full cleanup.
	for range 2 {
		if err := storage.Put(ctx, key, data, "image/jpeg"); err != nil {
			t.Fatal("B2 upload failed", err)
		}
	}
	actual, err := storage.Get(ctx, key)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("B2 photo read failed", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(endpoint, "/")+"/"+storage.bucket+"/"+key, nil)
	if err != nil {
		t.Fatal("invalid anonymous probe")
	}
	response, err := storage.controlHTTP.Do(request)
	if err != nil {
		t.Fatal("anonymous B2 probe failed")
	}
	response.Body.Close()
	if response.StatusCode != 403 && response.StatusCode != 401 {
		t.Fatalf("B2 anonymous read not denied, status %d", response.StatusCode)
	}
	if err := storage.Delete(ctx, key); err != nil {
		t.Fatal("B2 version deletion failed", err)
	}
	for object := range storage.client.ListObjects(ctx, storage.bucket, minio.ListObjectsOptions{
		Prefix: key, Recursive: true, WithVersions: true,
	}) {
		if object.Err != nil || object.Key == key {
			t.Fatal("B2 versions remain or verification failed")
		}
	}
	if ctx.Err() != nil {
		t.Fatal("B2 verification timed out")
	}
}
