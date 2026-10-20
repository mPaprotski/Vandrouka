package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/yourname/vandrouka/backend/internal/importer"
	"gopkg.in/yaml.v3"
)

// Loader encapsulates loading of reference data from the data directory.
type Loader struct {
	root string
}

// NewLoader creates a loader for the given data directory root.
func NewLoader(root string) *Loader {
	return &Loader{root: root}
}

// LoadPlaces reads all YAML place files under root/places/<city>/.
// Returns a map of city slug -> slice of Place.
func (l *Loader) LoadPlaces() (map[string][]importer.Place, error) {
	placesDir := filepath.Join(l.root, "places")
	entries, err := os.ReadDir(placesDir)
	if err != nil {
		return nil, fmt.Errorf("read places dir %q: %w", placesDir, err)
	}

	result := make(map[string][]importer.Place)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		city := entry.Name()
		cityDir := filepath.Join(placesDir, city)
		ps, err := l.loadPlacesFromCity(cityDir)
		if err != nil {
			return nil, fmt.Errorf("city %q: %w", city, err)
		}
		result[city] = ps
	}
	return result, nil
}

func (l *Loader) loadPlacesFromCity(dir string) ([]importer.Place, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []importer.Place
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		var p importer.Place
		if err := decoder.Decode(&p); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%s: expected exactly one YAML document", entry.Name())
		}
		result = append(result, p)
	}
	return result, nil
}

// LoadJSON reads a single JSON file from the data directory (e.g., res.json).
func (l *Loader) LoadJSON(filename string, target any) error {
	path := filepath.Join(l.root, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse %q: %w", filename, err)
	}
	return nil
}

// LoadAllJSON reads all .json files from the data directory into a map[filename]json.RawMessage.
func (l *Loader) LoadAllJSON() (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage)
	err := filepath.WalkDir(l.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		rel, err := filepath.Rel(l.root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return fmt.Errorf("parse %q: invalid JSON", rel)
		}
		result[rel] = data
		return nil
	})
	return result, err
}
