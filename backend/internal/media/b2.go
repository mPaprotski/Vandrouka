package media

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
)

// B2 exposes bucket privacy through its S3 ACL endpoint.
func (s *S3) checkB2Private(ctx context.Context) error {
	signed, err := s.client.Presign(ctx, http.MethodGet, s.bucket, "", time.Minute, url.Values{"acl": {""}})
	if err != nil {
		return ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, signed.String(), nil)
	if err != nil {
		return ErrUnavailable
	}
	response, err := s.controlHTTP.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	const maxACLBytes = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxACLBytes+1))
	if err != nil || len(body) > maxACLBytes {
		return ErrUnavailable
	}
	var policy struct {
		XMLName xml.Name `xml:"AccessControlPolicy"`
		OwnerID string   `xml:"Owner>ID"`
		Grants  []struct {
			ID         string `xml:"Grantee>ID"`
			URI        string `xml:"Grantee>URI"`
			Permission string `xml:"Permission"`
		} `xml:"AccessControlList>Grant"`
	}
	if err := xml.Unmarshal(body, &policy); err != nil || policy.OwnerID == "" || len(policy.Grants) != 1 {
		return ErrUnavailable
	}
	grant := policy.Grants[0]
	if grant.ID != policy.OwnerID || grant.URI != "" || grant.Permission != "FULL_CONTROL" {
		return ErrUnavailable
	}
	return nil
}

// B2's unversioned DELETE creates a hide marker. Purge exact-key versions so
// the durable deletion queue completes only after the stored bytes are removed.
func (s *S3) deleteB2Versions(ctx context.Context, key string) error {
	listingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var versions []minio.ObjectInfo
	for object := range s.client.ListObjects(listingCtx, s.bucket, minio.ListObjectsOptions{
		Prefix: key, Recursive: true, WithVersions: true,
	}) {
		if object.Err != nil {
			return ErrUnavailable
		}
		if object.Key != key {
			continue
		}
		if object.VersionID == "" {
			return ErrUnavailable
		}
		versions = append(versions, object)
	}
	// Finish pagination before changing the listing's continuation markers.
	for _, object := range versions {
		if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{VersionID: object.VersionID}); err != nil {
			return ErrUnavailable
		}
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	return nil
}
