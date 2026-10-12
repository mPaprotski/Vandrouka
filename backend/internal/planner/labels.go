package planner

// Labels compare final displayed variants, independently of generating profile.
type Label string

const (
	LabelBestMatch   Label = "Лучший по интересам"
	LabelMorePlaces  Label = "Больше мест"
	LabelLessWalking Label = "Меньше ходьбы"
	LabelBudget      Label = "Самый бюджетный"
)

// AssignLabels returns a copy, clearing old labels and requiring a strictly best
// result with at least 10% advantage over every other displayed variant. A lone
// variant has no comparative labels. Budget labels require all prices known.
func AssignLabels(variants []Variant) []Variant {
	result := append([]Variant(nil), variants...)
	for i := range result {
		result[i].Labels = nil
	}
	if len(result) < 2 {
		return result
	}
	allPricesKnown := true
	for _, v := range result {
		for _, stop := range v.Route.Stops {
			if stop.PriceUnconfirmed {
				allPricesKnown = false
			}
		}
	}
	criteria := []struct {
		label  Label
		higher bool
		value  func(Route) float64
	}{
		{LabelBestMatch, true, func(r Route) float64 { return r.Metrics.InterestValue }},
		{LabelMorePlaces, true, func(r Route) float64 { return float64(len(r.Stops)) }},
		{LabelLessWalking, false, func(r Route) float64 { return r.Metrics.WalkKM }},
		{LabelBudget, false, func(r Route) float64 { return float64(r.Metrics.CostMinor) }},
	}
	for _, criterion := range criteria {
		if criterion.label == LabelBudget && !allPricesKnown {
			continue
		}
		for i, v := range result {
			if len(v.Route.Stops) == 0 {
				continue
			}
			best := criterion.value(v.Route)
			qualifies := true
			for j, other := range result {
				if i == j {
					continue
				}
				rival := criterion.value(other.Route)
				// Relative advantage is measured against the other variant. An absolute
				// tolerance only protects floating arithmetic at exactly 10%, never ties.
				if criterion.higher {
					if best <= rival || best-rival+1e-12*mathScale(best, rival) < 0.1*rival {
						qualifies = false
						break
					}
				} else {
					if best >= rival || rival-best+1e-12*mathScale(best, rival) < 0.1*rival {
						qualifies = false
						break
					}
				}
			}
			if qualifies {
				result[i].Labels = append(result[i].Labels, criterion.label)
			}
		}
	}
	return result
}
func mathScale(a, b float64) float64 { return max(a, b) }
