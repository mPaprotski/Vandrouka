package exploration

import "testing"

func TestAchievementThresholds(t *testing.T) {
	for _, count := range []int64{0, 1, 4, 5, 9, 10, 11} {
		got := Definitions(count)
		if len(got) != 3 {
			t.Fatal(got)
		}
		for i, target := range []int64{1, 5, 10} {
			want := count
			if want > target {
				want = target
			}
			if got[i].Target != target || got[i].Progress != want || got[i].EarnedAt != nil {
				t.Fatal(count, got)
			}
		}
	}
}
