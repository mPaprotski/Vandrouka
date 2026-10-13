package apihttp

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func requestBody(changes map[string]any) string {
	fields := map[string]any{"start": map[string]any{"lat": 53.903333, "lon": 27.556111}, "start_at": "2026-10-24T08:00:00Z", "budget_minutes": 120}
	for k, v := range changes {
		fields[k] = v
	}
	data, _ := json.Marshal(fields)
	return string(data)
}
func TestRouteRequestDefaultsAndTimezone(t *testing.T) {
	r, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if r.City != "minsk" || r.Pace != "normal" || r.Seed != 42 || !r.IncludeGeometry || r.TicketBudgetMinor != nil || len(r.Categories) != 0 {
		t.Fatalf("wrong defaults: %+v", r)
	}
	if r.StartAt.Location().String() != "Europe/Minsk" || r.StartAt.Format("2006-01-02 15:04") != "2026-10-24 11:00" {
		t.Fatalf("timezone conversion failed: %s", r.StartAt)
	}
	r, err = ParseGenerateRouteRequest(strings.NewReader(requestBody(map[string]any{"start_at": "2026-10-24T23:30:00-04:00"})))
	if err != nil {
		t.Fatal(err)
	}
	if r.StartAt.Format("2006-01-02 15:04") != "2026-10-25 06:30" {
		t.Fatal("local date transition lost")
	}
	r, err = ParseGenerateRouteRequest(strings.NewReader(requestBody(map[string]any{"start_at": "2026-10-24t08:00:00.123456789z", "seed": 0, "include_geometry": false, "pace": "fast", "categories": []string{"museums", "parks"}})))
	if err != nil {
		t.Fatal(err)
	}
	if r.StartAt.Nanosecond() != 123456789 || r.Seed != 0 || r.IncludeGeometry || r.Pace != "fast" || len(r.Categories) != 2 {
		t.Fatal("explicit values or precision lost")
	}
}
func TestRouteTicketBudgetPresence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes map[string]any
		limited bool
		cost    int64
	}{
		{name: "omitted"}, {name: "null", changes: map[string]any{"ticket_budget_minor": nil}},
		{name: "zero", changes: map[string]any{"ticket_budget_minor": 0}, limited: true},
		{name: "exact kopecks", changes: map[string]any{"ticket_budget_minor": 2501}, limited: true, cost: 2501},
		{name: "int64 maximum", changes: map[string]any{"ticket_budget_minor": int64(math.MaxInt64)}, limited: true, cost: math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(tc.changes)))
			if err != nil {
				t.Fatal(err)
			}
			if (r.TicketBudgetMinor != nil) != tc.limited || tc.limited && *r.TicketBudgetMinor != tc.cost {
				t.Fatalf("wrong budget: %+v", r)
			}
		})
	}
}
func TestRouteRequestRejectsInvalidValues(t *testing.T) {
	cases := map[string]map[string]any{
		"zero budget": {"budget_minutes": 0}, "large budget": {"budget_minutes": 1441}, "fractional budget": {"budget_minutes": 1.5},
		"null required": {"budget_minutes": nil}, "null optional": {"pace": nil}, "null categories": {"categories": nil},
		"negative ticket": {"ticket_budget_minor": -1}, "fractional ticket": {"ticket_budget_minor": 1.1}, "overflow ticket": {"ticket_budget_minor": json.Number("9223372036854775808")},
		"negative seed": {"seed": -1}, "large seed": {"seed": MaxRouteSeed + 1}, "bad pace": {"pace": "running"},
		"duplicate category": {"categories": []string{"parks", "parks"}}, "unknown category": {"categories": []string{"food"}},
		"missing offset": {"start_at": "2026-10-24T11:00:00"}, "invalid date": {"start_at": "2026-02-30T11:00:00Z"}, "invalid offset": {"start_at": "2026-10-24T11:00:00+03:60"},
		"bad city": {"city": "Minsk"}, "empty city": {"city": ""}, "unknown key": {"access_token": "not-accepted"}, "case insensitive key": {"Seed": 42},
		"outside world": {"start": map[string]any{"lat": 91, "lon": 27.5}}, "outside Minsk": {"start": map[string]any{"lat": 53.7, "lon": 27.5}},
		"missing latitude": {"start": map[string]any{"lon": 27.5}}, "null coordinate": {"start": map[string]any{"lat": nil, "lon": 27.5}},
		"extra coordinate": {"start": map[string]any{"lat": 53.9, "lon": 27.5, "alt": 1}},
	}
	for name, changes := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(changes)))
			if !errors.Is(err, ErrInvalidRouteRequest) {
				t.Fatalf("accepted invalid request or wrong error: %v", err)
			}
		})
	}
	for _, key := range []string{"start", "start_at", "budget_minutes"} {
		t.Run("missing "+key, func(t *testing.T) {
			var fields map[string]any
			json.Unmarshal([]byte(requestBody(nil)), &fields)
			delete(fields, key)
			body, _ := json.Marshal(fields)
			_, err := ParseGenerateRouteRequest(strings.NewReader(string(body)))
			if !errors.Is(err, ErrInvalidRouteRequest) {
				t.Fatal(err)
			}
		})
	}
}
func TestRouteRequestRejectsAmbiguousJSON(t *testing.T) {
	valid := requestBody(nil)
	for _, body := range []string{"", "null", "[]", "true", valid + " {}", valid + " garbage", `{"start":`, strings.Replace(valid, `"budget_minutes":120`, `"budget_minutes":120,"budget_minutes":60`, 1), strings.Replace(valid, `"lat":53.903333`, `"lat":53.903333,"lat":53.9`, 1), string([]byte{0xff}), strings.Repeat(" ", MaxRouteRequestBytes) + valid} {
		if _, err := ParseGenerateRouteRequest(strings.NewReader(body)); !errors.Is(err, ErrInvalidRouteRequest) {
			t.Fatalf("accepted ambiguous/malformed JSON (%d bytes): %v", len(body), err)
		}
	}
	if _, err := ParseGenerateRouteRequest(strings.NewReader(valid + " \n\t")); err != nil {
		t.Fatal("trailing whitespace rejected", err)
	}
	if _, err := ParseGenerateRouteRequest(failingRouteReader{}); !errors.Is(err, ErrInvalidRouteRequest) {
		t.Fatal(err)
	}
}
func TestRouteRequestBoundsAndDeferredCityLookup(t *testing.T) {
	for _, budget := range []int{1, 1440} {
		for _, point := range []Coordinates{{Lat: 53.75, Lon: 27.3}, {Lat: 54.05, Lon: 27.85}} {
			if _, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(map[string]any{"budget_minutes": budget, "start": map[string]any{"lat": point.Lat, "lon": point.Lon}, "seed": MaxRouteSeed}))); err != nil {
				t.Fatal(err)
			}
		}
	}
	r, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(map[string]any{"city": "unknown-city"})))
	if err != nil || r.City != "unknown-city" {
		t.Fatal("city lookup must be left to the service", err)
	}
	r, err = r.InCityTimezone("Europe/Minsk")
	if err != nil || r.StartAt.Hour() != 11 {
		t.Fatal(err)
	}
	if _, err = r.InCityTimezone("Invalid/Timezone"); err == nil {
		t.Fatal("invalid server timezone accepted")
	}
}
func TestRouteValidationCopiesInput(t *testing.T) {
	var input GenerateRouteRequest
	json.Unmarshal([]byte(requestBody(map[string]any{"categories": []string{"parks"}, "ticket_budget_minor": 2501})), &input)
	r, err := ValidateGenerateRouteRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	r.Categories[0] = "museums"
	*r.TicketBudgetMinor = 0
	if input.Categories[0] != "parks" || *input.TicketBudgetMinor != 2501 {
		t.Fatal("validation retained mutable input aliases")
	}
	input.Start = &Coordinates{Lat: math.NaN(), Lon: 27.5}
	input.StartAt = modelPointer(time.Now())
	if _, err := ValidateGenerateRouteRequest(input); !errors.Is(err, ErrInvalidRouteRequest) {
		t.Fatal("NaN accepted")
	}
}

type failingRouteReader struct{}

func (failingRouteReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestReadRouteRequestHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		valid                   bool
	}{
		{"valid", "application/json; charset=utf-8", requestBody(nil), true},
		{"bad budget", "application/json", requestBody(map[string]any{"budget_minutes": 0}), false},
		{"missing content type", "", requestBody(nil), false},
		{"wrong content type", "text/plain", requestBody(nil), false},
		{"extra document", "application/json", requestBody(nil) + " {}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/routes/generate", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			_, ok := ReadGenerateRouteRequest(response, request)
			if ok != tc.valid {
				t.Fatalf("valid=%v want %v", ok, tc.valid)
			}
			if tc.valid {
				if response.Body.Len() != 0 {
					t.Fatal("valid read wrote a response")
				}
				return
			}
			var failure APIError
			if response.Code != 400 || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Error.Code != "invalid_request" {
				t.Fatalf("unexpected error envelope: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
