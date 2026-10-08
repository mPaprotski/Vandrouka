package routing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMatrix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/sources_to_targets" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var input struct {
			Costing string  `json:"costing"`
			Sources []Point `json:"sources"`
			Targets []Point `json:"targets"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.Costing != "pedestrian" || len(input.Sources) != 2 || len(input.Targets) != 2 {
			t.Errorf("unexpected input %+v", input)
		}
		_, _ = w.Write([]byte(`{"sources_to_targets":[[{"time":0,"distance":0},{"time":480,"distance":0.6}],[{"time":null,"distance":null},{"time":0,"distance":0}]]}`))
	}))
	defer server.Close()
	matrix, err := NewClient(server.URL, time.Second).Matrix(context.Background(), []Point{{Lat: 53.9, Lon: 27.5}, {Lat: 53.91, Lon: 27.51}})
	if err != nil {
		t.Fatal(err)
	}
	if matrix[0][1].Seconds != 480 || !matrix[0][1].Reachable || matrix[1][0].Reachable {
		t.Fatalf("incorrect matrix %+v", matrix)
	}
}

func TestMatrixErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"http error", "", 503}, {"bad JSON", "oops", 200}, {"wrong dimensions", `{"sources_to_targets":[]}`, 200},
		{"partial cell", `{"sources_to_targets":[[{"time":2,"distance":null}]]}`, 200},
		{"negative time", `{"sources_to_targets":[[{"time":-1,"distance":2}]]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := NewClient(server.URL, time.Second).Matrix(context.Background(), []Point{{Lat: 53.9, Lon: 27.5}})
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("expected unavailable, got %v", err)
			}
		})
	}
}

func TestContextAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, 20*time.Millisecond)
	_, err := client.Matrix(context.Background(), []Point{{Lat: 53.9, Lon: 27.5}})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected timeout, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Matrix(ctx, []Point{{Lat: 53.9, Lon: 27.5}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
