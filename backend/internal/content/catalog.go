// Package content enriches the database catalog with an optional, reviewed demo set.
// Images and their licence metadata stay in the external DATA_DIR.
package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
)

type Entry struct {
	Slug     string   `json:"slug"`
	Story    string   `json:"story"`
	Approach string   `json:"approach"`
	Sources  []string `json:"sources"`
	Cover    struct {
		Filename   string `json:"filename"`
		Author     string `json:"author"`
		License    string `json:"license"`
		LicenseURL string `json:"license_url"`
		SourceURL  string `json:"source_url"`
		SHA256     string `json:"sha256"`
		Changes    string `json:"changes"`
	} `json:"cover"`
}
type Manifest struct {
	City       string  `json:"city"`
	ReviewedOn string  `json:"reviewed_on"`
	Entries    []Entry `json:"entries"`
}
type asset struct {
	body     []byte
	etag     string
	modified time.Time
}
type Registry struct {
	entries map[string]*places.CuratedContent
	images  map[string]asset
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var filePattern = regexp.MustCompile(`^[a-z0-9-]+\.(jpg|png)$`)

func validURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

func Load(root string) (*Registry, error) {
	registry := &Registry{entries: map[string]*places.CuratedContent{}, images: map[string]asset{}}
	path := filepath.Join(root, "catalog", "minsk-demo.json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return registry, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("demo manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("demo manifest must contain one JSON document")
	}
	reviewed, err := time.Parse("2006-01-02", manifest.ReviewedOn)
	if err != nil || reviewed.After(time.Now().Add(24*time.Hour)) || manifest.City != "minsk" || len(manifest.Entries) < 5 || len(manifest.Entries) > 10 {
		return nil, fmt.Errorf("invalid demo city/date/size")
	}
	for index, entry := range manifest.Entries {
		key := manifest.City + "/" + entry.Slug
		if !slugPattern.MatchString(entry.Slug) || registry.entries[key] != nil || entry.Story == "" || entry.Approach == "" || len(entry.Sources) == 0 {
			return nil, fmt.Errorf("invalid/duplicate demo entry %q", entry.Slug)
		}
		for _, source := range entry.Sources {
			if !validURL(source) {
				return nil, fmt.Errorf("invalid source for %s", entry.Slug)
			}
		}
		cover := entry.Cover
		if !filePattern.MatchString(cover.Filename) || cover.Author == "" || cover.Changes == "" || !validURL(cover.LicenseURL) || !validURL(cover.SourceURL) {
			return nil, fmt.Errorf("invalid cover metadata for %s", entry.Slug)
		}
		licenses := map[string]string{"CC BY-SA 4.0": "https://creativecommons.org/licenses/by-sa/4.0/", "CC BY-SA 3.0": "https://creativecommons.org/licenses/by-sa/3.0/", "CC BY 4.0": "https://creativecommons.org/licenses/by/4.0/", "CC0": "https://creativecommons.org/publicdomain/zero/1.0/"}
		if expected, ok := licenses[cover.License]; !ok || expected != cover.LicenseURL {
			return nil, fmt.Errorf("unsupported licence for %s", entry.Slug)
		}
		if _, exists := registry.images[cover.Filename]; exists {
			return nil, fmt.Errorf("duplicate cover filename")
		}
		imagePath := filepath.Join(root, "covers", cover.Filename)
		info, err := os.Lstat(imagePath)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return nil, fmt.Errorf("invalid/missing cover %s", cover.Filename)
		}
		body, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		if hash != cover.SHA256 {
			return nil, fmt.Errorf("cover checksum mismatch for %s", entry.Slug)
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || (format != "jpeg" && format != "png") || (format == "jpeg" && filepath.Ext(cover.Filename) != ".jpg") || (format == "png" && filepath.Ext(cover.Filename) != ".png") || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
			return nil, fmt.Errorf("invalid cover image %s", cover.Filename)
		}
		registry.images[cover.Filename] = asset{body: body, etag: `"` + hash + `"`, modified: info.ModTime()}
		registry.entries[key] = &places.CuratedContent{Order: index + 1, Story: entry.Story, Approach: entry.Approach, Sources: entry.Sources, ReviewedOn: manifest.ReviewedOn, FieldVerified: false, Cover: places.Cover{URL: "/catalog-media/" + cover.Filename + "?v=" + hash[:12], Author: cover.Author, License: cover.License, LicenseURL: cover.LicenseURL, SourceURL: cover.SourceURL, Changes: cover.Changes}}
	}
	return registry, nil
}
func (r *Registry) enrich(place places.Place) places.Place {
	place.Content = r.entries[place.City+"/"+place.Slug]
	return place
}
func (r *Registry) Media(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name, matched := strings.CutPrefix(request.URL.Path, "/catalog-media/")
	if !matched {
		http.NotFound(w, request)
		return
	}
	value, found := r.images[name]
	if !found {
		http.NotFound(w, request)
		return
	}
	w.Header().Set("ETag", value.etag)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, request, name, value.modified, bytes.NewReader(value.body))
}

type Base interface {
	ListCities(context.Context) ([]places.City, error)
	ListCategories(context.Context, string) ([]places.Category, error)
	ListPlaces(context.Context, string, string, string) ([]places.Place, error)
	GetPlace(context.Context, int64) (places.Place, error)
}
type Catalog struct {
	Base
	Registry *Registry
}

func (c Catalog) ListPlaces(ctx context.Context, city, category, query string) ([]places.Place, error) {
	result, err := c.Base.ListPlaces(ctx, city, category, query)
	enriched := make([]places.Place, len(result))
	for index, place := range result {
		enriched[index] = c.Registry.enrich(place)
	}
	return enriched, err
}
func (c Catalog) GetPlace(ctx context.Context, id int64) (places.Place, error) {
	result, err := c.Base.GetPlace(ctx, id)
	if err != nil {
		return result, err
	}
	return c.Registry.enrich(result), nil
}
