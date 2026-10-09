package planner

import "time"

// CanVisit checks that the entire visit fits inside one opening interval.
// arrival must use the city's timezone. Unknown hours permit a visit, but the
// caller must flag it as unconfirmed. A dated exception takes precedence even
// when regular hours are unknown.
func CanVisit(place Place, arrival time.Time) bool {
	if arrival.IsZero() || place.VisitMinutes <= 0 || place.VisitMinutes > 1440 {
		return false
	}
	intervals, exception := place.Schedule.Exceptions[arrival.Format("2006-01-02")]
	if !exception {
		if !place.Schedule.Known {
			return true
		}
		intervals = place.Schedule.Weekly[arrival.Weekday()]
	}
	year, month, day := arrival.Date()
	departure := arrival.Add(time.Duration(place.VisitMinutes) * time.Minute)
	for _, interval := range intervals {
		if interval.OpenMinute < 0 || interval.CloseMinute > 1440 || interval.OpenMinute >= interval.CloseMinute {
			continue
		}
		opening := time.Date(year, month, day, interval.OpenMinute/60, interval.OpenMinute%60, 0, 0, arrival.Location())
		closing := time.Date(year, month, day, interval.CloseMinute/60, interval.CloseMinute%60, 0, 0, arrival.Location())
		if !arrival.Before(opening) && !departure.After(closing) {
			return true
		}
	}
	return false
}
