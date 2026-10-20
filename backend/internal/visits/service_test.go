package visits

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type captureRepo struct {
	command Command
	calls   int
}

func (r *captureRepo) Confirm(_ context.Context, c Command) (Result, error) {
	r.command = c
	r.calls++
	return Result{}, nil
}
func TestPolicyBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 123456789, time.UTC)
	p := DefaultPolicy()
	base := Location{Lat: 53, Lon: 27, AccuracyM: 30, ObservedAt: now}
	tests := []struct {
		name               string
		active, ready      bool
		distance, accuracy float64
		observed           time.Time
		want               RejectionCode
	}{
		{"exact radius accuracy age", true, true, 50, 30, now.Add(-120 * time.Second), ""},
		{"exact future", true, true, 50, 30, now.Add(30 * time.Second), ""},
		{"distance fraction", true, true, 50.000001, 30, now, TooFar},
		{"accuracy fraction", true, true, 0, 30.000001, now, PoorAccuracy},
		{"stale nanosecond", true, true, 0, 1, now.Add(-120*time.Second - time.Nanosecond), StaleLocation},
		{"future nanosecond", true, true, 0, 1, now.Add(30*time.Second + time.Nanosecond), FutureLocation},
		{"inactive precedence", false, false, 100, 100, now.Add(-time.Hour), InactivePlace},
		{"photo precedence", true, false, 100, 100, now, PhotoNotReady},
		{"accuracy precedes age", true, true, 100, 100, now.Add(-time.Hour), PoorAccuracy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := base
			l.AccuracyM = tt.accuracy
			l.ObservedAt = tt.observed
			if got := p.reject(tt.active, tt.ready, tt.distance, l, now); got != tt.want {
				t.Fatalf("%s != %s", got, tt.want)
			}
		})
	}
}
func TestDistance(t *testing.T) {
	// One degree at equator = mean radius * pi / 180; independently fixed reference.
	if got := DistanceM(0, 0, 0, 1); math.Abs(got-111195.080233533) > 0.000001 {
		t.Fatal(got)
	}
	if got := DistanceM(53, 27, 53, 27); got != 0 {
		t.Fatal(got)
	}
	if got := DistanceM(0, 179.999, 0, -179.999); math.Abs(got-222.390160467) > 0.00001 {
		t.Fatal(got)
	}
	if !finite(DistanceM(0, 0, 0, 180)) {
		t.Fatal("antipodes")
	}
}
func TestNormalizeAndValidate(t *testing.T) {
	repo := &captureRepo{}
	s, _ := New(repo, DefaultPolicy())
	now := time.Date(2026, 10, 25, 12, 0, 0, 123456789, time.UTC)
	in := Input{"AAAAAAAA-0000-4000-8000-000000000001", Location{53, 27, 0, now}}
	key := "BBBBBBBB-0000-4000-8000-000000000001"
	if _, err := s.Confirm(context.Background(), 1, 2, key, in, now); err != nil {
		t.Fatal(err)
	}
	hash := repo.command.Hash
	in.PhotoID = "aaaaaaaa-0000-4000-8000-000000000001"
	in.Location.ObservedAt = now.In(time.FixedZone("Minsk", 3*3600))
	s.Confirm(context.Background(), 1, 2, key, in, now.Add(time.Hour))
	if hash != repo.command.Hash {
		t.Fatal("offset/case/received time changed request hash")
	}
	if repo.command.ReceivedAt.Nanosecond() != now.Nanosecond() {
		t.Fatal("receive precision lost")
	}
	invalid := []Input{in, in, in, in, in, in, in}
	invalid[0].Location.Lat = math.NaN()
	invalid[1].Location.Lon = 181
	invalid[2].Location.AccuracyM = -1
	invalid[3].Location.AccuracyM = math.Inf(1)
	invalid[4].Location.ObservedAt = time.Time{}
	invalid[5].PhotoID = "not UUID"
	invalid[6].Location.Lat = -91
	before := repo.calls
	for _, v := range invalid {
		if _, err := s.Confirm(context.Background(), 1, 2, key, v, now); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if repo.calls != before {
		t.Fatal("invalid input reached repository")
	}
	for _, p := range []Policy{{}, {math.NaN(), 30, time.Second, 0}, {50, 30, time.Second, -time.Second}} {
		if _, err := New(repo, p); err == nil {
			t.Fatal("invalid policy")
		}
	}
}
