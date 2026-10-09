package planner

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

// DistanceMatrix provides walking travel times and distances between points.
// Row and column indices correspond to the order of places passed to the planner:
// - Index 0 is the starting point.
// - Index 1..N corresponds to Place[0]..Place[N-1].
type DistanceMatrix interface {
	Leg(from, to int) routing.Leg
}

// SliceMatrix wraps a 2D slice of routing.Leg as a DistanceMatrix.
type SliceMatrix [][]routing.Leg

func (m SliceMatrix) Leg(from, to int) routing.Leg {
	if from < 0 || from >= len(m) || to < 0 || to >= len(m[from]) {
		return routing.Leg{Reachable: false}
	}
	return m[from][to]
}

// MatrixBuilder calculates the routing matrix for start point and places.
type MatrixBuilder interface {
	BuildMatrix(ctx context.Context, start Point, places []Place) (DistanceMatrix, error)
}

// RouterMatrixBuilder uses a routing.Client to build a distance matrix.
type RouterMatrixBuilder struct {
	client routing.Router
}

func NewRouterMatrixBuilder(client routing.Router) *RouterMatrixBuilder {
	return &RouterMatrixBuilder{client: client}
}

func (b *RouterMatrixBuilder) BuildMatrix(ctx context.Context, start Point, plcs []Place) (DistanceMatrix, error) {
	points := make([]routing.Point, 1+len(plcs))
	points[0] = routing.Point{Lat: start.Lat, Lon: start.Lon}
	for i, p := range plcs {
		points[1+i] = routing.Point{Lat: p.Location.Lat, Lon: p.Location.Lon}
	}
	matrix, err := b.client.Matrix(ctx, points)
	if err != nil {
		return nil, err
	}
	return SliceMatrix(matrix), nil
}

// FromCatalogPlace converts a places.Place domain object from store/catalog
// into the planner.Place model.
func FromCatalogPlace(p places.Place) Place {
	res := Place{
		ID:           p.ID,
		Name:         p.Name,
		Location:     Point{Lat: p.Lat, Lon: p.Lon},
		Categories:   append([]string(nil), p.Categories...),
		Importance:   p.Importance,
		MustSee:      p.MustSee,
		VisitMinutes: p.VisitMinutes,
		PriceKnown:   p.PriceKnown,
		Schedule: Schedule{
			Known:      p.HoursKnown,
			Weekly:     make(map[time.Weekday][]Interval),
			Exceptions: make(map[string][]Interval),
		},
	}

	if p.PriceKnown {
		var selected *float64
		for _, pr := range p.Prices {
			if pr.Kind == "adult" && pr.Currency == "BYN" {
				val := pr.Amount
				selected = &val
				break
			}
		}
		if selected == nil {
			for _, pr := range p.Prices {
				if pr.Kind == "free" && pr.Currency == "BYN" {
					val := pr.Amount
					selected = &val
					break
				}
			}
		}
		if selected != nil && !math.IsNaN(*selected) && !math.IsInf(*selected, 0) && *selected >= 0 && *selected*100 < float64(math.MaxInt64) {
			res.TicketCostMinor = int64(math.Round(*selected * 100))
		} else {
			res.PriceKnown = false
		}
	}

	if p.HoursKnown {
		for _, h := range p.OpeningHours {
			openMin, err1 := parseClock(h.OpenTime, false)
			closeMin, err2 := parseClock(h.CloseTime, true)
			if err1 != nil || err2 != nil || openMin >= closeMin {
				continue
			}
			wd := time.Weekday(h.Weekday)
			res.Schedule.Weekly[wd] = append(res.Schedule.Weekly[wd], Interval{
				OpenMinute:  openMin,
				CloseMinute: closeMin,
			})
		}
	}

	for _, ex := range p.OpeningExceptions {
		if ex.Closed {
			res.Schedule.Exceptions[ex.Date] = nil
			continue
		}
		if ex.OpenTime != nil && ex.CloseTime != nil {
			openMin, err1 := parseClock(*ex.OpenTime, false)
			closeMin, err2 := parseClock(*ex.CloseTime, true)
			if err1 != nil || err2 != nil || openMin >= closeMin {
				continue
			}
			res.Schedule.Exceptions[ex.Date] = append(res.Schedule.Exceptions[ex.Date], Interval{
				OpenMinute:  openMin,
				CloseMinute: closeMin,
			})
		}
	}

	return res
}

func parseClock(s string, allow24 bool) (int, error) {
	if allow24 && s == "24:00" {
		return 1440, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}
