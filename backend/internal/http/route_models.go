package apihttp

import (
	"encoding/json"
	"time"
)

// GenerateRouteRequest retains presence for values whose defaults or required
// checks belong to request validation. TicketBudgetMinor nil means no limit;
// a pointer to zero means a zero budget. Monetary values are BYN kopecks.
type GenerateRouteRequest struct {
	City              *string      `json:"city,omitempty"`
	Start             *Coordinates `json:"start"`
	StartAt           *time.Time   `json:"start_at"`
	BudgetMinutes     *int         `json:"budget_minutes"`
	Pace              *string      `json:"pace,omitempty"`
	Categories        []string     `json:"categories,omitempty"`
	TicketBudgetMinor *int64       `json:"ticket_budget_minor,omitempty"`
	Seed              *int64       `json:"seed,omitempty"`
	IncludeGeometry   *bool        `json:"include_geometry,omitempty"`
}

type Coordinates struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type GenerateRouteResponse struct {
	Seed     int64          `json:"seed"`
	Timezone string         `json:"timezone"`
	Variants []RouteVariant `json:"variants"`
}

type RouteVariant struct {
	VariantID   string           `json:"variant_id"`
	Profile     string           `json:"profile"`
	Labels      []string         `json:"labels"`
	Stops       []RouteStop      `json:"stops"`
	Totals      RouteTotals      `json:"totals"`
	Explanation RouteExplanation `json:"explanation"`
	Geometry    *RouteGeometry   `json:"geometry"`
}

type RouteStop struct {
	PlaceID          int64       `json:"place_id"`
	Name             string      `json:"name"`
	Location         Coordinates `json:"location"`
	Arrival          time.Time   `json:"arrival"`
	Departure        time.Time   `json:"departure"`
	VisitSeconds     int         `json:"visit_seconds"`
	IncomingLeg      RouteLeg    `json:"incoming_leg"`
	HoursUnconfirmed bool        `json:"hours_unconfirmed"`
	PriceUnconfirmed bool        `json:"price_unconfirmed"`
}

// Fractional seconds and meters remain unrounded in transport models.
type RouteLeg struct {
	WalkSeconds float64 `json:"walk_seconds"`
	DistanceM   float64 `json:"distance_m"`
}

type RouteTotals struct {
	WalkSeconds          float64 `json:"walk_seconds"`
	VisitSeconds         int     `json:"visit_seconds"`
	TotalSeconds         float64 `json:"total_seconds"`
	DistanceM            float64 `json:"distance_m"`
	CostMinor            int64   `json:"cost_minor"`
	Currency             string  `json:"currency"`
	CostComplete         bool    `json:"cost_complete"`
	StopsCount           int     `json:"stops_count"`
	UnverifiedStopsCount int     `json:"unverified_stops_count"`
}

type RouteExplanation struct {
	Text                   string         `json:"text"`
	HoursUnconfirmedCount  int            `json:"hours_unconfirmed_count"`
	PricesUnconfirmedCount int            `json:"prices_unconfirmed_count"`
	CategoryCounts         map[string]int `json:"category_counts"`
}

type RouteGeometry struct {
	Format   string   `json:"format"`
	Segments []string `json:"segments"`
}

// APIError is shared by catalog and future route handlers. Details is optional.
type APIError struct {
	Error ErrorDetail `json:"error"`
}
type ErrorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Required collections serialize as []/{} even when a service returns no data.
// Geometry intentionally remains null when it is not provided. These methods
// normalize copies and do not mutate models supplied by their callers.
func (r GenerateRouteResponse) MarshalJSON() ([]byte, error) {
	type alias GenerateRouteResponse
	r.Variants = nonNilSlice(r.Variants)
	return json.Marshal(alias(r))
}
func (r RouteVariant) MarshalJSON() ([]byte, error) {
	type alias RouteVariant
	r.Labels = nonNilSlice(r.Labels)
	r.Stops = nonNilSlice(r.Stops)
	return json.Marshal(alias(r))
}
func (r RouteExplanation) MarshalJSON() ([]byte, error) {
	type alias RouteExplanation
	if r.CategoryCounts == nil {
		r.CategoryCounts = map[string]int{}
	}
	return json.Marshal(alias(r))
}
func (r RouteGeometry) MarshalJSON() ([]byte, error) {
	type alias RouteGeometry
	r.Segments = nonNilSlice(r.Segments)
	return json.Marshal(alias(r))
}
func nonNilSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
