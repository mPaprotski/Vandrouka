package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
)

func fixture(t *testing.T) (string, Manifest, []byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "covers"), 0755); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	body := encoded.Bytes()
	sum := sha256.Sum256(body)
	manifest := Manifest{City: "minsk", ReviewedOn: time.Now().UTC().Format("2006-01-02")}
	for i := 0; i < 5; i++ {
		entry := Entry{Slug: fmt.Sprintf("place-%d", i), Story: "A reviewed story", Approach: "Public pedestrian approach; not field verified", Sources: []string{"https://example.com/source"}}
		entry.Cover.Filename = entry.Slug + ".png"
		entry.Cover.Author = "Fixture author"
		entry.Cover.License = "CC BY-SA 4.0"
		entry.Cover.LicenseURL = "https://creativecommons.org/licenses/by-sa/4.0/"
		entry.Cover.SourceURL = "https://example.com/photo"
		entry.Cover.SHA256 = hex.EncodeToString(sum[:])
		entry.Cover.Changes = "Synthetic fixture"
		if err := os.WriteFile(filepath.Join(root, "covers", entry.Cover.Filename), body, 0600); err != nil {
			t.Fatal(err)
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	writeManifest(t, root, manifest)
	return root, manifest, body
}
func writeManifest(t *testing.T, root string, manifest Manifest) {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "catalog", "minsk-demo.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLoadDemoAndMedia(t *testing.T) {
	root, _, body := fixture(t)
	registry, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	p := registry.enrich(places.Place{ID: 1, City: "minsk", Slug: "place-0"})
	if p.Content == nil || p.Content.Order != 1 || p.Content.FieldVerified || p.Content.Cover.Author != "Fixture author" {
		t.Fatalf("bad enrichment: %+v", p.Content)
	}
	if registry.enrich(places.Place{City: "another", Slug: "place-0"}).Content != nil {
		t.Fatal("matched another city")
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/catalog-media/place-0.png", 200}, {"HEAD", "/catalog-media/place-0.png", 200}, {"GET", "/catalog-media/../catalog/minsk-demo.json", 404}, {"GET", "/catalog-media/missing.png", 404}, {"GET", "/", 404}, {"POST", "/catalog-media/place-0.png", 405}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			registry.Media(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("got %d", w.Code)
			}
			if tc.status == 200 {
				if w.Header().Get("ETag") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("missing headers")
				}
				if tc.method == "GET" && !bytes.Equal(w.Body.Bytes(), body) {
					t.Fatal("wrong image")
				}
				if tc.method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
			}
		})
	}
	w := httptest.NewRecorder()
	registry.Media(w, httptest.NewRequest("GET", "/catalog-media/place-0.png", nil))
	req := httptest.NewRequest("GET", "/catalog-media/place-0.png", nil)
	req.Header.Set("If-None-Match", w.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	registry.Media(cached, req)
	if cached.Code != http.StatusNotModified {
		t.Fatal("conditional cache miss")
	}
}
func TestBadManifestFailsAtomically(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Manifest)
	}{
		{"duplicate", func(m *Manifest) { m.Entries[1].Slug = m.Entries[0].Slug }},
		{"traversal", func(m *Manifest) { m.Entries[0].Cover.Filename = "../secret.png" }},
		{"checksum", func(m *Manifest) { m.Entries[0].Cover.SHA256 = "wrong" }},
		{"licence", func(m *Manifest) { m.Entries[0].Cover.License = "All rights reserved" }},
		{"licence URL", func(m *Manifest) { m.Entries[0].Cover.LicenseURL = "https://example.com/fake" }},
		{"unsafe source", func(m *Manifest) { m.Entries[0].Sources = []string{"javascript:alert(1)"} }},
		{"too few", func(m *Manifest) { m.Entries = m.Entries[:4] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, m, _ := fixture(t)
			test.change(&m)
			writeManifest(t, root, m)
			if registry, err := Load(root); err == nil || registry != nil {
				t.Fatal("accepted invalid/partial registry")
			}
		})
	}
	root, m, _ := fixture(t)
	file := filepath.Join(root, "covers", m.Entries[0].Cover.Filename)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "covers", m.Entries[1].Cover.Filename), file); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestOptionalManifest(t *testing.T) {
	r, err := Load(t.TempDir())
	if err != nil || len(r.entries) != 0 {
		t.Fatal("missing manifest must preserve base catalog")
	}
}

type baseCatalog struct{ items []places.Place }

func (b baseCatalog) ListCities(context.Context) ([]places.City, error) { return nil, nil }
func (b baseCatalog) ListCategories(context.Context, string) ([]places.Category, error) {
	return nil, nil
}
func (b baseCatalog) ListPlaces(context.Context, string, string, string) ([]places.Place, error) {
	return b.items, nil
}
func (b baseCatalog) GetPlace(context.Context, int64) (places.Place, error) { return b.items[0], nil }
func TestCatalogDoesNotMutateBase(t *testing.T) {
	root, _, _ := fixture(t)
	r, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	base := baseCatalog{[]places.Place{{ID: 1, City: "minsk", Slug: "place-0"}, {ID: 2, City: "minsk", Slug: "other"}}}
	c := Catalog{Base: base, Registry: r}
	result, err := c.ListPlaces(context.Background(), "minsk", "", "")
	if err != nil || len(result) != 2 || result[0].Content == nil || result[1].Content != nil || base.items[0].Content != nil {
		t.Fatal("invalid enrichment or base mutation")
	}
	detail, err := c.GetPlace(context.Background(), 1)
	if err != nil || detail.Content == nil {
		t.Fatal("detail not enriched")
	}
}
func TestExternalDemo(t *testing.T) {
	root := os.Getenv("TEST_DATA_DIR")
	if root == "" {
		t.Skip("TEST_DATA_DIR not set")
	}
	r, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.entries) != 5 {
		t.Fatalf("expected five demo entries, got %d", len(r.entries))
	}
}
