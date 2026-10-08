package importer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validPlace() Place {
	return Place{Slug: "victory-square", Name: "Площадь Победы", Summary: "Мемориальная площадь", Lat: 53.9084, Lon: 27.5756, Importance: 5, VisitMinutes: 20, HoursKnown: true, PriceKnown: true, VerifiedAt: "2026-10-08", Categories: []string{"monuments"}, OpeningHours: map[string][]string{"monday": {"00:00-24:00"}}, Prices: []Price{{Kind: "free", Currency: "BYN"}}, Links: []Link{{Kind: "official", URL: "https://minsk.gov.by"}}}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Place)
		valid  bool
	}{
		{"valid", func(p *Place) {}, true},
		{"missing name", func(p *Place) { p.Name = " " }, false},
		{"missing summary", func(p *Place) { p.Summary = "" }, false},
		{"outside city", func(p *Place) { p.Lat = 52 }, false},
		{"invalid importance", func(p *Place) { p.Importance = 6 }, false},
		{"invalid visit", func(p *Place) { p.VisitMinutes = 0 }, false},
		{"invalid date", func(p *Place) { p.VerifiedAt = "2026-02-30" }, false},
		{"future date", func(p *Place) { p.VerifiedAt = "2027-01-01" }, false},
		{"unknown category", func(p *Place) { p.Categories = []string{"unknown"} }, false},
		{"duplicate category", func(p *Place) { p.Categories = append(p.Categories, "monuments") }, false},
		{"unknown day", func(p *Place) { p.OpeningHours = map[string][]string{"foo": {"10:00-11:00"}} }, false},
		{"overnight", func(p *Place) { p.OpeningHours = map[string][]string{"monday": {"22:00-02:00"}} }, false},
		{"overlap", func(p *Place) { p.OpeningHours = map[string][]string{"monday": {"10:00-12:00", "11:00-13:00"}} }, false},
		{"unknown hours", func(p *Place) { p.HoursKnown = false }, false},
		{"unknown prices", func(p *Place) { p.PriceKnown = false }, false},
		{"negative price", func(p *Place) { p.Prices[0].Amount = -1 }, false},
		{"unsafe link", func(p *Place) { p.Links[0].URL = "javascript:alert(1)" }, false},
		{"closed exception", func(p *Place) { p.OpeningExceptions = []Exception{{Date: "2026-10-09", Closed: true}} }, true},
		{"open exception", func(p *Place) {
			p.OpeningExceptions = []Exception{{Date: "2026-10-09", Hours: []string{"10:00-12:00"}}}
		}, true},
		{"invalid exception", func(p *Place) {
			p.OpeningExceptions = []Exception{{Date: "2026-10-09", Closed: true, Hours: []string{"10:00-12:00"}}}
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validPlace()
			tc.change(&p)
			err := p.Validate(map[string]int64{"monuments": 1}, "minsk", time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
}

func TestReadDirRejectsInvalidFiles(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"unknown field", "slug: place\nextra: true\n"},
		{"multiple documents", "slug: place\n---\nslug: other\n"},
		{"duplicate key", "slug: place\nslug: other\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "place.yaml"), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadDir(dir); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	dir := t.TempDir()
	for _, name := range []string{"a.yaml", "b.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("slug: same\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReadDir(dir); err == nil {
		t.Fatal("expected duplicate slug error")
	}
}
