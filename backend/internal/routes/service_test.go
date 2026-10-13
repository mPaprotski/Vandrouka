package routes_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/store"
)

var _ routes.Catalog = (*store.Catalog)(nil)

type catalogStub struct {
	cities                []places.City
	cards                 []places.Place
	cityErr, placeErr     error
	cityCalls, placeCalls int
	receivedSlug          string
	cancel                context.CancelFunc
}

func (c *catalogStub) ListCities(ctx context.Context) ([]places.City, error) {
	c.cityCalls++
	return c.cities, c.cityErr
}
func (c *catalogStub) ListActivePlaces(ctx context.Context, slug string) ([]places.Place, error) {
	c.placeCalls++
	c.receivedSlug = slug
	if c.cancel != nil {
		c.cancel()
	}
	return c.cards, c.placeErr
}
func city() places.City { return places.City{ID: 1, Slug: "minsk", Timezone: "Europe/Minsk"} }
func card(id int64, categories ...string) places.Place {
	return places.Place{ID: id, Slug: "example", City: "minsk", Name: "Example", Lat: 53.9, Lon: 27.5, Importance: 3, VisitMinutes: 15, Categories: categories}
}
func request() planner.Request {
	return planner.Request{BudgetMinutes: 120, Start: planner.Point{Lat: 53.9, Lon: 27.5}, StartAt: time.Date(2026, 10, 24, 23, 30, 0, 0, time.FixedZone("source", -4*3600)), Pace: planner.PaceNormal, Seed: 42}
}
func TestPrepareCategoriesAndIndexMapping(t *testing.T) {
	a := card(70, "parks", "architecture")
	a.PriceKnown = true
	a.Prices = []places.Price{{Kind: "adult", Amount: 25.01, Currency: "BYN"}}
	a.HoursKnown = true
	a.OpeningHours = []places.Hours{{Weekday: 0, OpenTime: "10:00", CloseTime: "19:00"}}
	closed := places.Exception{Date: "2026-10-25", Closed: true}
	a.OpeningExceptions = []places.Exception{closed}
	catalog := &catalogStub{cities: []places.City{city()}, cards: []places.Place{card(90, "streets"), a, card(20, "museums")}}
	req := request()
	req.Categories = []string{"parks", "museums"}
	zero := int64(0)
	req.MaxTicketCostMinor = &zero
	prepared, err := routes.NewService(catalog).Prepare(context.Background(), "minsk", req)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.cityCalls != 1 || catalog.placeCalls != 1 || catalog.receivedSlug != "minsk" {
		t.Fatal("category union must load the catalog once")
	}
	if len(prepared.Places) != 2 || prepared.Places[0].ID != 70 || prepared.Places[1].ID != 20 || prepared.MatrixIndices[70] != 1 || prepared.MatrixIndices[20] != 2 {
		t.Fatalf("mapping/order changed: %+v", prepared)
	}
	if len(prepared.Cards) != 2 || prepared.Cards[70].Name != a.Name {
		t.Fatal("card correspondence missing")
	}
	p := prepared.Places[0]
	if !p.PriceKnown || p.TicketCostMinor != 2501 || !p.Schedule.Known || len(p.Schedule.Weekly[time.Sunday]) != 1 {
		t.Fatal("catalog conversion lost price/schedule")
	}
	if hours, ok := p.Schedule.Exceptions["2026-10-25"]; !ok || len(hours) != 0 {
		t.Fatal("closed exception lost")
	}
	if prepared.Request.StartAt.Location().String() != "Europe/Minsk" || prepared.Request.StartAt.Format("2006-01-02 15:04") != "2026-10-25 06:30" {
		t.Fatal("city timezone/date lost")
	}
	prepared.Request.Categories[0] = "streets"
	*prepared.Request.MaxTicketCostMinor = 100
	prepared.Cards[70].Categories[0] = "religious"
	prepared.Cards[70].Prices[0].Amount = 0
	if req.Categories[0] != "parks" || zero != 0 || catalog.cards[1].Categories[0] != "parks" || catalog.cards[1].Prices[0].Amount != 25.01 {
		t.Fatal("prepared data shares mutable catalog/request state")
	}
}
func TestPrepareEmptyAndAllCategories(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cards      []places.Place
		categories []string
		count      int
	}{
		{name: "empty catalog"}, {name: "no category match", cards: []places.Place{card(1, "parks")}, categories: []string{"museums"}},
		{name: "all categories", cards: []places.Place{card(1, "parks"), card(2, "museums")}, count: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := &catalogStub{cities: []places.City{city()}, cards: tc.cards}
			req := request()
			req.Categories = tc.categories
			p, err := routes.NewService(catalog).Prepare(context.Background(), "minsk", req)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Places) != tc.count || p.Empty() != (tc.count == 0) || p.Places == nil || p.Cards == nil || p.MatrixIndices == nil {
				t.Fatal("empty success/mapping differs")
			}
		})
	}
}
func TestPrepareUnknownCityAndErrors(t *testing.T) {
	boom := errors.New("catalog failure")
	for _, tc := range []struct {
		name       string
		catalog    catalogStub
		want       error
		placeCalls int
	}{
		{name: "unknown city", want: routes.ErrCityNotFound},
		{name: "city repository error", catalog: catalogStub{cityErr: boom}, want: boom},
		{name: "place repository error", catalog: catalogStub{cities: []places.City{city()}, placeErr: boom}, want: boom, placeCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := routes.NewService(&tc.catalog).Prepare(context.Background(), "minsk", request())
			if !errors.Is(err, tc.want) || tc.catalog.placeCalls != tc.placeCalls {
				t.Fatalf("error=%v calls=%d", err, tc.catalog.placeCalls)
			}
		})
	}
	for _, timezone := range []string{"", "Local", "Unknown/Timezone"} {
		c := city()
		c.Timezone = timezone
		catalog := &catalogStub{cities: []places.City{c}}
		if _, err := routes.NewService(catalog).Prepare(context.Background(), "minsk", request()); err == nil || catalog.placeCalls != 0 {
			t.Fatal("invalid metadata must fail before loading places")
		}
	}
}
func TestPrepareDuplicatePolicyAndInvalidCatalog(t *testing.T) {
	catalog := &catalogStub{cities: []places.City{city()}, cards: []places.Place{card(1, "parks"), card(1, "museums"), card(2, "museums")}}
	req := request()
	req.Categories = []string{"museums"}
	p, err := routes.NewService(catalog).Prepare(context.Background(), "minsk", req)
	if err != nil || len(p.Places) != 1 || p.Places[0].ID != 2 {
		t.Fatal("first catalog entry must win before filtering", err)
	}
	for _, bad := range []places.Place{card(0, "parks"), {ID: 1, City: "other"}} {
		catalog.cards = []places.Place{bad}
		if _, err := routes.NewService(catalog).Prepare(context.Background(), "minsk", request()); err == nil {
			t.Fatal("invalid ID/cross-city record accepted")
		}
	}
}
func TestPrepareCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	catalog := &catalogStub{cities: []places.City{city()}}
	if _, err := routes.NewService(catalog).Prepare(ctx, "minsk", request()); !errors.Is(err, context.Canceled) || catalog.cityCalls != 0 {
		t.Fatal("cancelled preparation accessed repository")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	catalog.cancel = cancel
	if _, err := routes.NewService(catalog).Prepare(ctx, "minsk", request()); !errors.Is(err, context.Canceled) {
		t.Fatal("repository cancellation was ignored")
	}
}
