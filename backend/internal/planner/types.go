// Package planner contains route planning rules independent of HTTP and storage.
package planner

import "time"

type Point struct {
	Lat float64
	Lon float64
}

type Pace string

const (
	PaceSlow   Pace = "slow"
	PaceNormal Pace = "normal"
	PaceFast   Pace = "fast"
)

// WalkMultiplier scales the routing service's normal walking time.
func (p Pace) WalkMultiplier() float64 {
	switch p {
	case PaceSlow:
		return 1.2
	case PaceFast:
		return 0.85
	default:
		return 1
	}
}

// Interval uses minutes after local midnight. CloseMinute may equal 1440.
// Overnight hours must be split into separate days.
type Interval struct {
	OpenMinute  int
	CloseMinute int
}

// Schedule dates and weekdays refer to the city's local timezone.
// Exceptions replace the weekly hours; an empty interval list means closed.
type Schedule struct {
	Known      bool
	Weekly     map[time.Weekday][]Interval
	Exceptions map[string][]Interval // YYYY-MM-DD
}

type Place struct {
	ID              int64
	Name            string
	Location        Point
	Categories      []string
	Importance      int
	MustSee         bool
	VisitMinutes    int
	Schedule        Schedule
	PriceKnown      bool
	TicketCostMinor int64 // BYN kopecks; integer arithmetic avoids rounding errors.
}

type Profile string

const (
	ProfileBestMatch   Profile = "best_match"
	ProfileMorePlaces  Profile = "more_places"
	ProfileLessWalking Profile = "less_walking"
	ProfileBudget      Profile = "budget"
)

type Request struct {
	Profile            Profile
	Categories         []string
	BudgetMinutes      int
	Start              Point
	StartAt            time.Time
	Pace               Pace
	MaxTicketCostMinor *int64 // nil means no ticket budget limit.
	Seed               int64
}

type Stop struct {
	PlaceID          int64
	Arrival          time.Time
	Departure        time.Time
	HoursUnconfirmed bool
	PriceUnconfirmed bool
}

type Metrics struct {
	InterestValue   float64
	WalkMinutes     float64
	VisitMinutes    int
	TotalMinutes    float64
	WalkKM          float64
	CostMinor       int64
	UnverifiedCount int
	CategoryCounts  map[string]int
	Score           float64
}

type Route struct {
	Stops   []Stop
	Metrics Metrics
	Seed    int64
}
