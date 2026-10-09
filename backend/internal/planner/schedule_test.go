package planner

import (
	"testing"
	"time"
)

func TestCanVisit(t *testing.T) {
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	regular := Schedule{Known: true, Weekly: map[time.Weekday][]Interval{
		time.Friday: {{600, 780}, {840, 1080}},
	}}
	tests := []struct {
		name     string
		schedule Schedule
		arrival  string
		minutes  int
		want     bool
	}{
		{"open", regular, "2026-10-09T10:00:00", 60, true},
		{"before opening", regular, "2026-10-09T09:59:00", 60, false},
		{"ends at closing", regular, "2026-10-09T12:00:00", 60, true},
		{"too late", regular, "2026-10-09T12:01:00", 60, false},
		{"seconds matter", regular, "2026-10-09T12:00:01", 60, false},
		{"lunch break", regular, "2026-10-09T12:30:00", 120, false},
		{"second interval", regular, "2026-10-09T14:00:00", 60, true},
		{"closed weekday", regular, "2026-10-10T10:00:00", 60, false},
		{"unknown hours", Schedule{}, "2026-10-09T10:00:00", 60, true},
		{"holiday closure", Schedule{Exceptions: map[string][]Interval{"2026-10-09": nil}}, "2026-10-09T10:00:00", 60, false},
		{"holiday opening", Schedule{Known: true, Exceptions: map[string][]Interval{"2026-10-10": {{600, 720}}}}, "2026-10-10T10:00:00", 60, true},
		{"exception replaces weekly", Schedule{Known: true, Weekly: regular.Weekly, Exceptions: map[string][]Interval{"2026-10-09": {{840, 900}}}}, "2026-10-09T10:00:00", 60, false},
		{"ends at midnight", Schedule{Known: true, Weekly: map[time.Weekday][]Interval{time.Friday: {{0, 1440}}}}, "2026-10-09T23:00:00", 60, true},
		{"crosses midnight", Schedule{Known: true, Weekly: map[time.Weekday][]Interval{time.Friday: {{0, 1440}}}}, "2026-10-09T23:30:00", 60, false},
		{"invalid interval", Schedule{Known: true, Weekly: map[time.Weekday][]Interval{time.Friday: {{600, 1500}}}}, "2026-10-09T10:00:00", 60, false},
		{"zero duration", regular, "2026-10-09T10:00:00", 0, false},
		{"negative duration", Schedule{}, "2026-10-09T10:00:00", -1, false},
		{"overflow duration", Schedule{}, "2026-10-09T10:00:00", int(^uint(0) >> 1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arrival, err := time.ParseInLocation("2006-01-02T15:04:05", tt.arrival, minsk)
			if err != nil {
				t.Fatal(err)
			}
			place := Place{VisitMinutes: tt.minutes, Schedule: tt.schedule}
			if got := CanVisit(place, arrival); got != tt.want {
				t.Fatalf("CanVisit = %v, want %v", got, tt.want)
			}
		})
	}
	if CanVisit(Place{VisitMinutes: 60}, time.Time{}) {
		t.Fatal("zero arrival must be rejected")
	}
}

func TestPaceWalkMultiplier(t *testing.T) {
	for pace, want := range map[Pace]float64{PaceSlow: 1.2, PaceNormal: 1, PaceFast: 0.85, "": 1} {
		if got := pace.WalkMultiplier(); got != want {
			t.Errorf("pace %q: got %v, want %v", pace, got, want)
		}
	}
}
