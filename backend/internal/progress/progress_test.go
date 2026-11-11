package progress

import (
	"math"
	"testing"
)

func TestLevelBoundaries(t *testing.T) {
	// Fixed expectations independent of the implementation table.
	cases := []struct {
		xp                     int64
		level                  int
		start, next, remaining int64
	}{
		{0, 1, 0, 100, 100}, {99, 1, 0, 100, 1}, {100, 2, 100, 300, 200}, {101, 2, 100, 300, 199},
		{299, 2, 100, 300, 1}, {300, 3, 300, 600, 300}, {301, 3, 300, 600, 299},
		{599, 3, 300, 600, 1}, {600, 4, 600, 1000, 400}, {601, 4, 600, 1000, 399},
		{999, 4, 600, 1000, 1}, {1000, 5, 1000, 1500, 500}, {1001, 5, 1000, 1500, 499},
		{1499, 5, 1000, 1500, 1}, {1500, 6, 1500, 2100, 600}, {1501, 6, 1500, 2100, 599},
		{2099, 6, 1500, 2100, 1}, {2100, 7, 2100, 2800, 700}, {2101, 7, 2100, 2800, 699},
		{2799, 7, 2100, 2800, 1}, {2800, 8, 2800, 3600, 800}, {2801, 8, 2800, 3600, 799},
		{3599, 8, 2800, 3600, 1}, {3600, 9, 3600, 4500, 900}, {3601, 9, 3600, 4500, 899},
		{4499, 9, 3600, 4500, 1}, {4500, 10, 4500, 0, 0}, {4501, 10, 4500, 0, 0}, {math.MaxInt64, 10, 4500, 0, 0},
	}
	for _, c := range cases {
		s, err := Calculate(c.xp)
		if err != nil || s.TotalXP != c.xp || s.Level != c.level || s.LevelStartXP != c.start {
			t.Fatal(c, s, err)
		}
		if c.level == 10 {
			if s.NextLevelXP != nil || s.XPToNextLevel != nil {
				t.Fatal(s)
			}
		} else if s.NextLevelXP == nil || s.XPToNextLevel == nil || *s.NextLevelXP != c.next || *s.XPToNextLevel != c.remaining {
			t.Fatal(c, s)
		}
	}
	if _, err := Calculate(-1); err == nil {
		t.Fatal("negative XP accepted")
	}
}
