// Package plannercheck runs reproducible, offline acceptance scenarios.
package plannercheck

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/yourname/vandrouka/backend/internal/importer"
	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routing"
	"gopkg.in/yaml.v3"
)

type Scenario struct {
	Name              string        `yaml:"name"`
	BudgetMinutes     int           `yaml:"budget_minutes"`
	Categories        []string      `yaml:"categories"`
	Pace              planner.Pace  `yaml:"pace"`
	TicketBudgetMinor *int64        `yaml:"ticket_budget_minor"`
	Start             routing.Point `yaml:"start"`
	StartAt           time.Time     `yaml:"start_at"`
	Seed              int64         `yaml:"seed"`
}
type Catalog struct {
	Places []planner.Place
	Slugs  map[int64]string
}

func read(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(out)
}
func LoadScenarios(path string) ([]Scenario, error) {
	var scenarios []Scenario
	if err := read(path, &scenarios); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	location, err := time.LoadLocation("Europe/Minsk")
	if err != nil {
		return nil, err
	}
	for i, s := range scenarios {
		if s.Name == "" || seen[s.Name] {
			return nil, fmt.Errorf("empty or repeated scenario name")
		}
		seen[s.Name] = true
		scenarios[i].StartAt = s.StartAt.In(location)
	}
	return scenarios, nil
}
func (s Scenario) Request() planner.Request {
	return planner.Request{BudgetMinutes: s.BudgetMinutes, Categories: s.Categories, Pace: s.Pace, MaxTicketCostMinor: s.TicketBudgetMinor, Start: planner.Point{Lat: s.Start.Lat, Lon: s.Start.Lon}, StartAt: s.StartAt, Seed: s.Seed}
}
func LoadCatalog(path string) (Catalog, error) {
	var raw []importer.Place
	if err := read(path, &raw); err != nil {
		return Catalog{}, err
	}
	catalog := Catalog{Slugs: map[int64]string{}}
	categories := map[string]int64{"museums": 1, "parks": 2, "architecture": 3, "religious": 4, "monuments": 5, "streets": 6}
	weekdays := map[string]int{"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6}
	seen := map[string]bool{}
	for i, p := range raw {
		if err := p.Validate(categories, "minsk", time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)); err != nil {
			return Catalog{}, fmt.Errorf("%s: %w", p.Slug, err)
		}
		if seen[p.Slug] {
			return Catalog{}, fmt.Errorf("duplicate catalog slug %s", p.Slug)
		}
		seen[p.Slug] = true
		id := int64(i + 1)
		catalog.Slugs[id] = p.Slug
		domain := places.Place{ID: id, Name: p.Name, Lat: p.Lat, Lon: p.Lon, Categories: p.Categories, Importance: p.Importance, MustSee: p.MustSee, VisitMinutes: p.VisitMinutes, HoursKnown: p.HoursKnown, PriceKnown: p.PriceKnown}
		for day, intervals := range p.OpeningHours {
			for _, interval := range intervals {
				pair := strings.Split(interval, "-")
				domain.OpeningHours = append(domain.OpeningHours, places.Hours{Weekday: weekdays[day], OpenTime: pair[0], CloseTime: pair[1]})
			}
		}
		for _, ex := range p.OpeningExceptions {
			if ex.Closed {
				domain.OpeningExceptions = append(domain.OpeningExceptions, places.Exception{Date: ex.Date, Closed: true})
				continue
			}
			for _, interval := range ex.Hours {
				pair := strings.Split(interval, "-")
				domain.OpeningExceptions = append(domain.OpeningExceptions, places.Exception{Date: ex.Date, OpenTime: &pair[0], CloseTime: &pair[1]})
			}
		}
		for _, price := range p.Prices {
			domain.Prices = append(domain.Prices, places.Price{Kind: price.Kind, Amount: price.Amount, Currency: price.Currency})
		}
		catalog.Places = append(catalog.Places, planner.FromCatalogPlace(domain))
	}
	return catalog, nil
}

// SyntheticMatrix is a deterministic directed model, NOT a street network.
// Distances use a local plane at 54N, a 1.25 detour factor and a 0..4% directed
// offset. Walking speed is 4.8 km/h. This model cannot validate real access.
func SyntheticMatrix(req planner.Request, catalog Catalog) planner.SliceMatrix {
	points := []planner.Point{req.Start}
	for _, p := range catalog.Places {
		points = append(points, p.Location)
	}
	matrix := make(planner.SliceMatrix, len(points))
	for i, a := range points {
		matrix[i] = make([]routing.Leg, len(points))
		for j, b := range points {
			km := math.Hypot((a.Lat-b.Lat)*111.32, (a.Lon-b.Lon)*65.60) * 1.25 * (1 + float64((i*7+j*3)%5)/100)
			matrix[i][j] = routing.Leg{Reachable: true, Kilometers: km, Seconds: km * 750}
		}
	}
	return matrix
}
