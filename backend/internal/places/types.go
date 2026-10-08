package places

import "time"

type City struct {
	ID        int64   `json:"id"`
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	CenterLat float64 `json:"center_lat"`
	CenterLon float64 `json:"center_lon"`
	Timezone  string  `json:"timezone"`
}

type Category struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	PlacesCount int    `json:"places_count"`
}

type Place struct {
	ID                int64       `json:"id"`
	Slug              string      `json:"slug"`
	City              string      `json:"city"`
	Name              string      `json:"name"`
	Summary           string      `json:"summary"`
	Lat               float64     `json:"lat"`
	Lon               float64     `json:"lon"`
	Importance        int         `json:"importance"`
	MustSee           bool        `json:"must_see"`
	VisitMinutes      int         `json:"visit_minutes"`
	HoursKnown        bool        `json:"hours_known"`
	PriceKnown        bool        `json:"price_known"`
	VerifiedAt        string      `json:"verified_at"`
	Categories        []string    `json:"categories"`
	OpeningHours      []Hours     `json:"opening_hours"`
	OpeningExceptions []Exception `json:"opening_exceptions"`
	Prices            []Price     `json:"prices"`
	Links             []Link      `json:"links"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

type Hours struct {
	Weekday   int    `json:"weekday"`
	OpenTime  string `json:"open_time"`
	CloseTime string `json:"close_time"`
}
type Exception struct {
	Date      string  `json:"date"`
	Closed    bool    `json:"closed"`
	OpenTime  *string `json:"open_time"`
	CloseTime *string `json:"close_time"`
	Note      string  `json:"note"`
}
type Price struct {
	Kind     string  `json:"kind"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}
type Link struct {
	Kind  string `json:"kind"`
	URL   string `json:"url"`
	Title string `json:"title"`
}
