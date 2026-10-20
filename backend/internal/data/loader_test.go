package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReferenceFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "places", "minsk")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	record := []byte("slug: example\nname: Example\nvisit_minutes: 15\nmust_see: true\nopening_hours:\n  monday: [10:00-18:00]\n")
	if err := os.WriteFile(filepath.Join(dir, "example.yaml"), record, 0600); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(root)
	places, err := loader.LoadPlaces()
	if err != nil {
		t.Fatal(err)
	}
	p := places["minsk"][0]
	if p.VisitMinutes != 15 || !p.MustSee || p.OpeningHours["monday"][0] != "10:00-18:00" {
		t.Fatalf("wrong YAML mapping: %+v", p)
	}
	path := filepath.Join(root, "res.json")
	if err := os.WriteFile(path, []byte(`{"count":60}`), 0600); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Count int `json:"count"`
	}
	if err := loader.LoadJSON("res.json", &result); err != nil || result.Count != 60 {
		t.Fatalf("JSON: %+v %v", result, err)
	}
	all, err := loader.LoadAllJSON()
	if err != nil || len(all) != 1 {
		t.Fatalf("JSON files: %v %v", all, err)
	}
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.LoadAllJSON(); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestExternalCatalog(t *testing.T) {
	root := os.Getenv("TEST_DATA_DIR")
	if root == "" {
		t.Skip("TEST_DATA_DIR is not set")
	}
	catalog, err := NewLoader(root).LoadPlaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog["minsk"]) != 60 {
		t.Fatalf("got %d Minsk places", len(catalog["minsk"]))
	}
	if _, err := NewLoader(root).LoadAllJSON(); err != nil {
		t.Fatal(err)
	}
}
