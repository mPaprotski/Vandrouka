package routes_test

import (
	"context"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
	"testing"
)

func TestFinalDirectionsEnforceDistanceLimit(t *testing.T) {
	catalog, req, m := candidateFixture()
	// The .75km Matrix route fits, but actual Directions now exceeds the limit.
	limit := 800.0
	req.MaxDistanceM = &limit
	path := directionPath(200, 200)
	path.Legs[0].Kilometers = 0.7
	path.Legs[1].Kilometers = 0.3
	directions := &directionsStub{paths: []routing.Route{path}}
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(&matrixRouter{matrix: m})), routes.WithDirections(directions))
	result, err := service.Generate(context.Background(), "minsk", req, true)
	if err != nil || len(result.Variants) != 0 {
		t.Fatalf("final distance limit ignored: %+v %v", result, err)
	}
}
