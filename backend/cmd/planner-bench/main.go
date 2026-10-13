// planner-bench produces the day-25 acceptance report; live requests are opt-in.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/plannercheck"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

type result struct {
	Name      string            `json:"name"`
	Budget    int               `json:"budget_minutes"`
	ComputeMS float64           `json:"compute_ms"`
	Rejected  int               `json:"rejected_by_directions"`
	Variants  []planner.Variant `json:"variants"`
}
type report struct {
	Source      string           `json:"matrix_source"`
	CatalogSize int              `json:"catalog_size"`
	Slugs       map[int64]string `json:"slugs"`
	Results     []result         `json:"results"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	requests := flag.String("requests", "testdata/golden_requests.yaml", "fixed requests")
	catalogFile := flag.String("catalog", "testdata/golden_catalog.yaml", "frozen catalog")
	selected := flag.String("cases", "", "comma-separated scenario names; empty means all")
	live := flag.Bool("mapbox", false, "explicitly make billed Matrix/Directions requests")
	output := flag.String("output", "", "JSON report path; empty writes to stdout")
	golden := flag.String("write-golden", "", "write reviewed offline baseline (no timing)")
	flag.Parse()
	if *live && *golden != "" {
		return fmt.Errorf("golden results must use the offline matrix")
	}
	scenarios, err := plannercheck.LoadScenarios(*requests)
	if err != nil {
		return err
	}
	catalog, err := plannercheck.LoadCatalog(*catalogFile)
	if err != nil {
		return err
	}
	if *selected != "" {
		wanted := map[string]bool{}
		for _, name := range strings.Split(*selected, ",") {
			wanted[name] = true
		}
		var filtered []plannercheck.Scenario
		for _, s := range scenarios {
			if wanted[s.Name] {
				filtered = append(filtered, s)
				delete(wanted, s.Name)
			}
		}
		if len(wanted) > 0 {
			return fmt.Errorf("unknown scenario selection")
		}
		scenarios = filtered
	}
	r := report{Source: "synthetic-directed-v1", CatalogSize: len(catalog.Places), Slugs: catalog.Slugs}
	var client *routing.MapboxClient
	var liveMatrix routing.Matrix
	startIndices := map[routing.Point]int{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if *live {
		if os.Getenv("MAPBOX_ACCESS_TOKEN") == "" {
			return fmt.Errorf("MAPBOX_ACCESS_TOKEN is required for -mapbox")
		}
		client = routing.NewMapboxClient(os.Getenv("MAPBOX_URL"), os.Getenv("MAPBOX_ACCESS_TOKEN"), 20*time.Second)
		var points []routing.Point
		for _, s := range scenarios {
			if _, ok := startIndices[s.Start]; !ok {
				startIndices[s.Start] = len(points)
				points = append(points, s.Start)
			}
		}
		for _, p := range catalog.Places {
			points = append(points, routing.Point{Lat: p.Location.Lat, Lon: p.Location.Lon})
		}
		fmt.Fprintf(os.Stderr, "Mapbox Matrix: %d points (%d elements); Directions for selected variants\n", len(points), len(points)*len(points))
		liveMatrix, err = client.Matrix(ctx, points)
		if err != nil {
			return err
		}
		r.Source = "mapbox-matrix-and-directions"
	}
	baseline := map[string][]planner.Variant{}
	for _, s := range scenarios {
		req := s.Request()
		matrix := plannercheck.SyntheticMatrix(req, catalog)
		if *live {
			indices := []int{startIndices[s.Start]}
			for i := range catalog.Places {
				indices = append(indices, len(startIndices)+i)
			}
			for i := range matrix {
				for j := range matrix[i] {
					matrix[i][j] = liveMatrix[indices[i]][indices[j]]
				}
			}
		}
		p := planner.New()
		started := time.Now()
		variants, err := p.PlanVariants(req, catalog.Places, matrix, true)
		elapsed := float64(time.Since(started).Nanoseconds()) / 1e6
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		again, err := p.PlanVariants(req, catalog.Places, matrix, true)
		if err != nil || !reflect.DeepEqual(variants, again) {
			return fmt.Errorf("%s: non-deterministic result", s.Name)
		}
		entry := result{Name: s.Name, Budget: req.BudgetMinutes, ComputeMS: elapsed}
		for _, v := range variants {
			auditMatrix := matrix
			if *live {
				index := map[int64]int{}
				for i, place := range catalog.Places {
					index[place.ID] = i + 1
				}
				points := []routing.Point{s.Start}
				var order []int64
				for _, stop := range v.Route.Stops {
					place := catalog.Places[index[stop.PlaceID]-1]
					points = append(points, routing.Point{Lat: place.Location.Lat, Lon: place.Location.Lon})
					order = append(order, stop.PlaceID)
				}
				directions, err := client.Route(ctx, points)
				if err != nil {
					return fmt.Errorf("%s Directions: %w", s.Name, err)
				}
				if len(directions.Legs) != len(order) {
					return fmt.Errorf("Directions leg count differs")
				}
				auditMatrix = make(planner.SliceMatrix, len(matrix))
				for i := range matrix {
					auditMatrix[i] = append([]routing.Leg(nil), matrix[i]...)
				}
				from := 0
				for i, id := range order {
					auditMatrix[from][index[id]] = directions.Legs[i]
					from = index[id]
				}
				candidateReq := req
				candidateReq.Profile = v.Profile
				checked, err := p.EvaluateOrder(candidateReq, catalog.Places, auditMatrix, order)
				if errors.Is(err, planner.ErrInfeasible) {
					entry.Rejected++
					continue
				}
				if err != nil {
					return err
				}
				v.Route = checked
			}
			if err := plannercheck.Audit(req, catalog, auditMatrix, v.Route); err != nil {
				return fmt.Errorf("%s: %w", s.Name, err)
			}
			entry.Variants = append(entry.Variants, v)
		}
		if len(entry.Variants) == 0 {
			return fmt.Errorf("%s: no feasible variants", s.Name)
		}
		entry.Variants = planner.AssignLabels(entry.Variants)
		baseline[s.Name] = entry.Variants
		r.Results = append(r.Results, entry)
		fmt.Fprintf(os.Stderr, "%s: %d variants checked, %d rejected by Directions\n", s.Name, len(entry.Variants), entry.Rejected)
	}
	if *golden != "" {
		if err := writeJSON(*golden, baseline); err != nil {
			return err
		}
	}
	return writeJSON(*output, r)
}
func writeJSON(path string, data any) error {
	writer := os.Stdout
	if path != "" {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}
