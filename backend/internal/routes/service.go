// Package routes connects catalog data to route planning independently of HTTP.
package routes

import (
	"context"
	"errors"
	"fmt"
	"time"
	_ "time/tzdata"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
)

var ErrCityNotFound = errors.New("route city not found")

// Catalog returns active places only, in stable repository order. Filtering
// multiple categories is a union handled by the service, without repeat queries.
type Catalog interface {
	ListCities(context.Context) ([]places.City, error)
	ListActivePlaces(context.Context, string) ([]places.Place, error)
}

type Service struct{ catalog Catalog }

func NewService(catalog Catalog) *Service { return &Service{catalog: catalog} }

// Prepared preserves correspondence between original cards, Planner places and
// the future routing matrix: index 0 is start, indices 1..N are Places order.
// An empty Places slice is a successful preparation, not an unknown city.
type Prepared struct {
	City          places.City
	Request       planner.Request
	Places        []planner.Place
	Cards         map[int64]places.Place
	MatrixIndices map[int64]int
}

func (p Prepared) Empty() bool { return len(p.Places) == 0 }

// Prepare loads the city and active catalog, applies category selection, and
// converts the data. Matrix calls and PlanVariants belong to the next step.
func (s *Service) Prepare(ctx context.Context, citySlug string, request planner.Request) (Prepared, error) {
	result := Prepared{Request: request, Places: []planner.Place{}, Cards: map[int64]places.Place{}, MatrixIndices: map[int64]int{}}
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	if s == nil || s.catalog == nil {
		return Prepared{}, fmt.Errorf("route catalog is not configured")
	}
	cities, err := s.catalog.ListCities(ctx)
	if err != nil {
		return Prepared{}, fmt.Errorf("load route cities: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	found := false
	for _, city := range cities {
		if city.Slug == citySlug {
			result.City = city
			found = true
			break
		}
	}
	if !found {
		return Prepared{}, ErrCityNotFound
	}
	if result.City.Timezone == "" || result.City.Timezone == "Local" {
		return Prepared{}, fmt.Errorf("invalid city timezone")
	}
	location, err := time.LoadLocation(result.City.Timezone)
	if err != nil {
		return Prepared{}, fmt.Errorf("load route timezone: %w", err)
	}
	result.Request.StartAt = request.StartAt.In(location)
	result.Request.Categories = append([]string(nil), request.Categories...)
	if request.MaxTicketCostMinor != nil {
		cost := *request.MaxTicketCostMinor
		result.Request.MaxTicketCostMinor = &cost
	}
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	cards, err := s.catalog.ListActivePlaces(ctx, citySlug)
	if err != nil {
		return Prepared{}, fmt.Errorf("load active route places: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	selected := map[string]bool{}
	for _, category := range request.Categories {
		selected[category] = true
	}
	seen := map[int64]bool{}
	for _, card := range cards {
		if card.ID <= 0 || card.City != citySlug {
			return Prepared{}, fmt.Errorf("invalid route catalog record")
		}
		// First occurrence wins, matching Planner's documented duplicate policy.
		// Mark before category filtering so a conflicting second record cannot win.
		if seen[card.ID] {
			continue
		}
		seen[card.ID] = true
		matches := len(selected) == 0
		for _, category := range card.Categories {
			if selected[category] {
				matches = true
			}
		}
		if !matches {
			continue
		}
		card = cloneCard(card)
		result.Places = append(result.Places, planner.FromCatalogPlace(card))
		result.Cards[card.ID] = card
		result.MatrixIndices[card.ID] = len(result.Places)
	}
	return result, nil
}
func cloneCard(card places.Place) places.Place {
	card.Categories = append([]string(nil), card.Categories...)
	card.OpeningHours = append([]places.Hours(nil), card.OpeningHours...)
	card.OpeningExceptions = append([]places.Exception(nil), card.OpeningExceptions...)
	for i := range card.OpeningExceptions {
		ex := &card.OpeningExceptions[i]
		if ex.OpenTime != nil {
			value := *ex.OpenTime
			ex.OpenTime = &value
		}
		if ex.CloseTime != nil {
			value := *ex.CloseTime
			ex.CloseTime = &value
		}
	}
	card.Prices = append([]places.Price(nil), card.Prices...)
	card.Links = append([]places.Link(nil), card.Links...)
	return card
}
