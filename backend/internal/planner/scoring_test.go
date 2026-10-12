package planner

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestConfigurableScoreAndRecalculation(t *testing.T) {
	req := auditRequest()
	a, b := auditPlace(1), auditPlace(2)
	a.Categories = []string{"museums"}
	a.Schedule = Schedule{Known: true, Weekly: map[time.Weekday][]Interval{time.Monday: {{0, 1440}}}}
	b.MustSee = true
	b.TicketCostMinor = 200
	m := optMatrix(map[[2]int]float64{{0, 1}: 5, {1, 2}: 10})
	p, err := NewWithWeights(ScoringWeights{WalkKM: 2, CostBYN: 3, Unverified: 4})
	if err != nil {
		t.Fatal(err)
	}
	route, err := p.Plan(req, []Place{a, b}, m)
	if err != nil {
		t.Fatal(err)
	}
	// value=5+7.5=12.5; walk=1.5km; cost=2 BYN; one unverified stop.
	// 12.5 - 2*1.5 - 3*2 - 4*1 = -0.5.
	if route.Metrics.InterestValue != 12.5 || route.Metrics.Score != -0.5 {
		t.Fatalf("wrong score: %+v", route.Metrics)
	}
	evaluated, err := p.EvaluateOrder(req, []Place{a, b}, m, ids(route))
	if err != nil || !reflect.DeepEqual(route, evaluated) {
		t.Fatalf("inconsistent score after evaluation: %+v %v", evaluated, err)
	}
	noPenalty, err := NewWithWeights(ScoringWeights{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := noPenalty.Plan(req, []Place{a, b}, m)
	if err != nil || raw.Metrics.Score != 12.5 {
		t.Fatalf("zero coefficients ignored: %+v %v", raw, err)
	}
}
func TestInvalidScoringWeights(t *testing.T) {
	for _, w := range []ScoringWeights{{WalkKM: -1}, {CostBYN: math.NaN()}, {Unverified: math.Inf(1)}} {
		if _, err := NewWithWeights(w); err == nil {
			t.Fatalf("invalid weights accepted: %+v", w)
		}
	}
}
func labelVariant(value, km float64, cost int64, count int) Variant {
	r := Route{Metrics: Metrics{InterestValue: value, WalkKM: km, CostMinor: cost}}
	for i := 0; i < count; i++ {
		r.Stops = append(r.Stops, Stop{PlaceID: int64(i + 1)})
	}
	return Variant{Route: r}
}
func hasLabel(v Variant, label Label) bool {
	for _, l := range v.Labels {
		if l == label {
			return true
		}
	}
	return false
}

func TestLabelsThresholds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		a, b  Variant
		label Label
		want  bool
	}{
		{"interest exact 10 percent", labelVariant(11, 1, 100, 1), labelVariant(10, 1, 100, 1), LabelBestMatch, true},
		{"interest below threshold", labelVariant(10.99, 1, 100, 1), labelVariant(10, 1, 100, 1), LabelBestMatch, false},
		{"walk exact 10 percent", labelVariant(1, 0.9, 100, 1), labelVariant(1, 1, 100, 1), LabelLessWalking, true},
		{"walk below threshold", labelVariant(1, 0.901, 100, 1), labelVariant(1, 1, 100, 1), LabelLessWalking, false},
		{"cost exact 10 percent", labelVariant(1, 1, 90, 1), labelVariant(1, 1, 100, 1), LabelBudget, true},
		{"cost below threshold", labelVariant(1, 1, 91, 1), labelVariant(1, 1, 100, 1), LabelBudget, false},
		{"free beats paid", labelVariant(1, 1, 0, 1), labelVariant(1, 1, 100, 1), LabelBudget, true},
		{"free tie", labelVariant(1, 1, 0, 1), labelVariant(1, 1, 0, 1), LabelBudget, false},
		{"more places exact 10 percent", labelVariant(1, 1, 100, 11), labelVariant(1, 1, 100, 10), LabelMorePlaces, true},
		{"more places below threshold", labelVariant(1, 1, 100, 12), labelVariant(1, 1, 100, 11), LabelMorePlaces, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := AssignLabels([]Variant{tc.a, tc.b})
			if hasLabel(out[0], tc.label) != tc.want || hasLabel(out[1], tc.label) {
				t.Fatalf("wrong labels: %+v", out)
			}
		})
	}
}
func TestLabelsCompareEveryFinalVariant(t *testing.T) {
	a, b, c := labelVariant(12, 1, 0, 1), labelVariant(10, 1, 100, 1), labelVariant(11.5, 1, 100, 1)
	out := AssignLabels([]Variant{a, b, c})
	if hasLabel(out[0], LabelBestMatch) {
		t.Fatal("10 percent against only one rival is insufficient")
	}
	b.Route.Stops[0].PriceUnconfirmed = true
	out = AssignLabels([]Variant{a, b})
	if hasLabel(out[0], LabelBudget) {
		t.Fatal("unconfirmed rival price makes budget comparison unreliable")
	}
	a.Labels = []Label{LabelBudget}
	lone := AssignLabels([]Variant{a})
	if len(lone[0].Labels) != 0 || !hasLabel(a, LabelBudget) {
		t.Fatal("singleton labels or input mutation")
	}
}
