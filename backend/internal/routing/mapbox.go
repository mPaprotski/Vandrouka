package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrAuthentication = errors.New("Mapbox token rejected")
	ErrRateLimited    = errors.New("Mapbox rate limit reached")
	ErrNoRoute        = errors.New("no walking route")
)

// MapboxClient implements pedestrian Matrix and Directions without exposing
// access tokens in errors. Requests are paced below Matrix's 60/minute limit.
// No automatic retries are made: each retry can incur additional billing.
type MapboxClient struct {
	baseURL  string
	token    string
	http     *http.Client
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func NewMapboxClient(baseURL, token string, timeout time.Duration) *MapboxClient {
	if baseURL == "" {
		baseURL = "https://api.mapbox.com"
	}
	return &MapboxClient{baseURL: strings.TrimRight(baseURL, "/"), token: strings.TrimSpace(token), http: &http.Client{Timeout: timeout}, interval: 1050 * time.Millisecond}
}
func (c *MapboxClient) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	when := c.next
	if when.Before(now) {
		when = now
	}
	c.next = when.Add(c.interval)
	c.mu.Unlock()
	timer := time.NewTimer(time.Until(when))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (c *MapboxClient) get(ctx context.Context, path string, q url.Values, output any) error {
	if c.token == "" {
		return fmt.Errorf("%w: MAPBOX_ACCESS_TOKEN is required", ErrAuthentication)
	}
	if err := c.wait(ctx); err != nil {
		return err
	}
	q.Set("access_token", c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("%w: invalid Mapbox endpoint", ErrUnavailable)
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// net/url.Error contains the URL and token: never propagate it.
		return fmt.Errorf("%w: Mapbox request failed", ErrUnavailable)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %w", ErrUnavailable, ErrAuthentication)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrUnavailable, ErrRateLimited)
	case http.StatusOK:
	default:
		return fmt.Errorf("%w: Mapbox HTTP %d", ErrUnavailable, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(output); err != nil {
		return fmt.Errorf("%w: invalid Mapbox response", ErrUnavailable)
	}
	return nil
}
func coordinates(points []Point) string {
	items := make([]string, len(points))
	for i, p := range points {
		items[i] = strconv.FormatFloat(p.Lon, 'f', -1, 64) + "," + strconv.FormatFloat(p.Lat, 'f', -1, 64)
	}
	return strings.Join(items, ";")
}
func indices(items []int) string {
	parts := make([]string, len(items))
	for i, n := range items {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ";")
}
func (c *MapboxClient) Matrix(ctx context.Context, points []Point) (Matrix, error) {
	return c.Between(ctx, points, points)
}

// Between assembles directed rectangular blocks using at most 25 coordinates.
// For small catalogs, all coordinates share one request. Larger inputs use
// blocks of 12 sources and 12 targets, preserving the original row/column order.
func (c *MapboxClient) Between(ctx context.Context, sources, targets []Point) (Matrix, error) {
	if err := validatePoints(sources); err != nil {
		return nil, err
	}
	if err := validatePoints(targets); err != nil {
		return nil, err
	}
	if c.token == "" {
		return nil, fmt.Errorf("%w: MAPBOX_ACCESS_TOKEN is required", ErrAuthentication)
	}
	unique := make(map[Point]bool)
	for _, p := range sources {
		unique[p] = true
	}
	for _, p := range targets {
		unique[p] = true
	}
	if len(unique) <= 25 {
		return c.block(ctx, sources, targets)
	}
	result := make(Matrix, len(sources))
	for i := range result {
		result[i] = make([]Leg, len(targets))
	}
	const chunk = 12
	for a := 0; a < len(sources); a += chunk {
		for b := 0; b < len(targets); b += chunk {
			srcEnd := min(a+chunk, len(sources))
			dstEnd := min(b+chunk, len(targets))
			block, err := c.block(ctx, sources[a:srcEnd], targets[b:dstEnd])
			if err != nil {
				return nil, err
			}
			for i := range block {
				copy(result[a+i][b:dstEnd], block[i])
			}
		}
	}
	return result, nil
}
func (c *MapboxClient) block(ctx context.Context, sources, targets []Point) (Matrix, error) {
	var points []Point
	lookup := make(map[Point]int)
	add := func(p Point) int {
		if i, ok := lookup[p]; ok {
			return i
		}
		i := len(points)
		lookup[p] = i
		points = append(points, p)
		return i
	}
	src := make([]int, len(sources))
	dst := make([]int, len(targets))
	for i, p := range sources {
		src[i] = add(p)
	}
	for i, p := range targets {
		dst[i] = add(p)
	}
	result := make(Matrix, len(sources))
	for i := range result {
		result[i] = make([]Leg, len(targets))
	}
	if len(points) == 1 {
		for i := range result {
			for j := range result[i] {
				result[i][j] = Leg{Reachable: true}
			}
		}
		return result, nil
	}
	// Mapbox requires at least two returned elements, even for a 1x1 query.
	querySrc, srcPosition := uniqueIndices(src)
	queryDst, dstPosition := uniqueIndices(dst)
	if len(querySrc)*len(queryDst) == 1 {
		queryDst = append(queryDst, src[0])
	}
	q := url.Values{"sources": {indices(querySrc)}, "destinations": {indices(queryDst)}, "annotations": {"duration,distance"}}
	var out struct {
		Code      string       `json:"code"`
		Durations [][]*float64 `json:"durations"`
		Distances [][]*float64 `json:"distances"`
	}
	if err := c.get(ctx, "/directions-matrix/v1/mapbox/walking/"+coordinates(points), q, &out); err != nil {
		return nil, err
	}
	if out.Code == "NoRoute" {
		return result, nil
	}
	if out.Code != "Ok" || len(out.Durations) != len(querySrc) || len(out.Distances) != len(querySrc) {
		return nil, fmt.Errorf("%w: invalid Mapbox matrix", ErrUnavailable)
	}
	for i := range src {
		row := srcPosition[src[i]]
		if len(out.Durations[row]) != len(queryDst) || len(out.Distances[row]) != len(queryDst) {
			return nil, fmt.Errorf("%w: invalid Mapbox matrix dimensions", ErrUnavailable)
		}
		for j := range dst {
			col := dstPosition[dst[j]]
			seconds, meters := out.Durations[row][col], out.Distances[row][col]
			if seconds == nil && meters == nil {
				continue
			}
			if seconds == nil || meters == nil || !validNumber(*seconds) || !validNumber(*meters) {
				return nil, fmt.Errorf("%w: invalid Mapbox matrix cell", ErrUnavailable)
			}
			result[i][j] = Leg{Seconds: *seconds, Kilometers: *meters / 1000, Reachable: true}
		}
	}
	return result, nil
}
func validNumber(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 }

// Route chunks long open paths at shared endpoints and returns polyline6 shapes.
// Summaries use the provider's normal pace; planner applies pace independently.
func (c *MapboxClient) Route(ctx context.Context, points []Point) (Route, error) {
	if err := validatePoints(points); err != nil {
		return Route{}, err
	}
	if len(points) < 2 {
		return Route{}, fmt.Errorf("route requires at least two points")
	}
	result := Route{}
	for start := 0; start < len(points)-1; {
		end := min(start+25, len(points))
		var out struct {
			Code   string `json:"code"`
			Routes []struct {
				Duration *float64 `json:"duration"`
				Distance *float64 `json:"distance"`
				Geometry string   `json:"geometry"`
				Legs     []struct {
					Duration *float64 `json:"duration"`
					Distance *float64 `json:"distance"`
				} `json:"legs"`
			} `json:"routes"`
		}
		q := url.Values{"geometries": {"polyline6"}, "overview": {"full"}, "steps": {"false"}, "alternatives": {"false"}}
		if err := c.get(ctx, "/directions/v5/mapbox/walking/"+coordinates(points[start:end]), q, &out); err != nil {
			return Route{}, err
		}
		if out.Code == "NoRoute" {
			return Route{}, ErrNoRoute
		}
		if out.Code != "Ok" || len(out.Routes) == 0 {
			return Route{}, fmt.Errorf("%w: invalid Mapbox directions", ErrUnavailable)
		}
		route := out.Routes[0]
		if route.Duration == nil || route.Distance == nil || !validNumber(*route.Duration) || !validNumber(*route.Distance) || route.Geometry == "" || len(route.Legs) != end-start-1 {
			return Route{}, fmt.Errorf("%w: invalid Mapbox route", ErrUnavailable)
		}
		result.Seconds += *route.Duration
		result.Kilometers += *route.Distance / 1000
		result.Shapes = append(result.Shapes, route.Geometry)
		for _, leg := range route.Legs {
			if leg.Duration == nil || leg.Distance == nil || !validNumber(*leg.Duration) || !validNumber(*leg.Distance) {
				return Route{}, fmt.Errorf("%w: invalid Mapbox route leg", ErrUnavailable)
			}
			result.Legs = append(result.Legs, Leg{Seconds: *leg.Duration, Kilometers: *leg.Distance / 1000, Reachable: true})
		}
		start = end - 1
	}
	return result, nil
}

func uniqueIndices(items []int) ([]int, map[int]int) {
	var result []int
	position := make(map[int]int)
	for _, item := range items {
		if _, exists := position[item]; !exists {
			position[item] = len(result)
			result = append(result, item)
		}
	}
	return result, position
}
