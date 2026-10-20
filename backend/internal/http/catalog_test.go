package apihttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/importer"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/store"
	"gopkg.in/yaml.v3"
)

func TestCatalogIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	fixture, err := os.ReadFile("../../testdata/golden_catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var data []importer.Place
	err = yaml.Unmarshal(fixture, &data)
	if err != nil {
		t.Fatal(err)
	}
	// The checked-in catalog includes the GPX review dated 13 October.
	if err := importer.Import(ctx, pool, "minsk", data, time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	RegisterCatalog(router, store.NewCatalog(pool))
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/v1/cities", 200, `"timezone":"Europe/Minsk"`},
		{"/v1/cities/minsk/categories", 200, `"places_count":8`},
		{"/v1/cities/minsk/places?category=museums", 200, `"name":"Национальный художественный музей"`},
		{"/v1/cities/minsk/places?q=ПОБЕДЫ", 200, `"slug":"victory-square"`},
		{"/v1/cities/minsk/places?q=%25", 200, `"places":[]`},
		{"/v1/cities/minsk/places?category=unknown", 400, `"code":"invalid_request"`},
		{"/v1/cities/unknown/places", 404, `"code":"not_found"`},
		{"/v1/cities/unknown/categories", 404, `"code":"not_found"`},
		{"/v1/places/-1", 400, `"code":"invalid_request"`},
		{"/v1/places/999999999999999999", 404, `"code":"not_found"`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if !json.Valid(w.Body.Bytes()) {
				t.Fatal("response is not JSON")
			}
		})
	}
	var id int64
	if err := pool.QueryRow(ctx, "SELECT id FROM places WHERE slug='national-art-museum'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	card, err := store.NewCatalog(pool).GetPlace(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(card.OpeningHours) != 6 || len(card.Prices) != 1 || card.Prices[0].Amount != 15 || len(card.Links) != 3 {
		t.Fatalf("related card data missing: %+v", card)
	}
	// An inactive place must disappear from both discovery and direct lookup.
	if _, err := pool.Exec(ctx, "UPDATE places SET is_active=false WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), "UPDATE places SET is_active=true WHERE id=$1", id)
	prepared, err := routes.NewService(store.NewCatalog(pool)).Prepare(ctx, "minsk", planner.Request{BudgetMinutes: 120, Start: planner.Point{Lat: 53.903333, Lon: 27.556111}, StartAt: time.Date(2026, 10, 24, 11, 0, 0, 0, time.UTC), Pace: planner.PaceNormal, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := prepared.Cards[id]; exists {
		t.Fatal("inactive place entered route catalog")
	}
	if _, exists := prepared.MatrixIndices[id]; exists {
		t.Fatal("inactive place entered route matrix")
	}
	for _, place := range prepared.Places {
		if place.ID == id {
			t.Fatal("inactive place entered Planner data")
		}
	}
	if _, err := store.NewCatalog(pool).GetPlace(ctx, id); err != store.ErrNotFound {
		t.Fatalf("inactive card is accessible: %v", err)
	}
	list, err := store.NewCatalog(pool).ListPlaces(ctx, "minsk", "museums", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.ID == id {
			t.Fatal("inactive place is listed")
		}
	}
}
