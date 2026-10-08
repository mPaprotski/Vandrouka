package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("routing unavailable")

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}
type Leg struct {
	Seconds    float64 `json:"seconds"`
	Kilometers float64 `json:"kilometers"`
	Reachable  bool    `json:"reachable"`
}
type Matrix [][]Leg

type Router interface {
	Matrix(context.Context, []Point) (Matrix, error)
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}}
}

func (c *Client) request(ctx context.Context, path string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: invalid endpoint", ErrUnavailable)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", ErrUnavailable, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(output); err != nil {
		return fmt.Errorf("%w: invalid response: %v", ErrUnavailable, err)
	}
	return nil
}

func validatePoints(points []Point) error {
	if len(points) == 0 || len(points) > 100 {
		return fmt.Errorf("expected 1–100 points")
	}
	for _, p := range points {
		if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || math.Abs(p.Lat) > 90 || math.Abs(p.Lon) > 180 {
			return fmt.Errorf("invalid coordinates")
		}
	}
	return nil
}

func (c *Client) Matrix(ctx context.Context, points []Point) (Matrix, error) {
	return c.Between(ctx, points, points)
}

// Between also supports start-to-places queries without rebuilding the places matrix.
func (c *Client) Between(ctx context.Context, sources, targets []Point) (Matrix, error) {
	if err := validatePoints(sources); err != nil {
		return nil, err
	}
	if err := validatePoints(targets); err != nil {
		return nil, err
	}
	input := struct {
		Sources []Point `json:"sources"`
		Targets []Point `json:"targets"`
		Costing string  `json:"costing"`
		Units   string  `json:"units"`
		Verbose bool    `json:"verbose"`
	}{sources, targets, "pedestrian", "kilometers", true}
	var output struct {
		Rows [][]struct {
			Time     *float64 `json:"time"`
			Distance *float64 `json:"distance"`
		} `json:"sources_to_targets"`
	}
	if err := c.request(ctx, "/sources_to_targets", input, &output); err != nil {
		return nil, err
	}
	if len(output.Rows) != len(sources) {
		return nil, fmt.Errorf("%w: wrong matrix rows", ErrUnavailable)
	}
	matrix := make(Matrix, len(sources))
	for i, row := range output.Rows {
		if len(row) != len(targets) {
			return nil, fmt.Errorf("%w: wrong matrix columns", ErrUnavailable)
		}
		matrix[i] = make([]Leg, len(targets))
		for j, cell := range row {
			if cell.Time == nil && cell.Distance == nil {
				continue
			}
			if cell.Time == nil || cell.Distance == nil || *cell.Time < 0 || *cell.Distance < 0 {
				return nil, fmt.Errorf("%w: invalid matrix cell", ErrUnavailable)
			}
			matrix[i][j] = Leg{Seconds: *cell.Time, Kilometers: *cell.Distance, Reachable: true}
		}
	}
	return matrix, nil
}

type Route struct {
	Shapes     []string `json:"shapes"`
	Seconds    float64  `json:"seconds"`
	Kilometers float64  `json:"kilometers"`
}

func (c *Client) Route(ctx context.Context, points []Point) (Route, error) {
	if err := validatePoints(points); err != nil {
		return Route{}, err
	}
	if len(points) < 2 {
		return Route{}, fmt.Errorf("route requires at least two points")
	}
	var output struct {
		Trip struct {
			Summary struct {
				Time   float64 `json:"time"`
				Length float64 `json:"length"`
			} `json:"summary"`
			Legs []struct {
				Shape string `json:"shape"`
			} `json:"legs"`
		} `json:"trip"`
	}
	input := struct {
		Locations []Point `json:"locations"`
		Costing   string  `json:"costing"`
		Units     string  `json:"units"`
	}{points, "pedestrian", "kilometers"}
	if err := c.request(ctx, "/route", input, &output); err != nil {
		return Route{}, err
	}
	if len(output.Trip.Legs) != len(points)-1 {
		return Route{}, fmt.Errorf("%w: wrong route legs", ErrUnavailable)
	}
	route := Route{Shapes: make([]string, 0, len(output.Trip.Legs)), Seconds: output.Trip.Summary.Time, Kilometers: output.Trip.Summary.Length}
	for _, leg := range output.Trip.Legs {
		if leg.Shape == "" {
			return Route{}, fmt.Errorf("%w: missing shape", ErrUnavailable)
		}
		route.Shapes = append(route.Shapes, leg.Shape)
	}
	return route, nil
}
