package planner

import (
	"reflect"
	"strings"
	"testing"
)

func TestExplainVerifiedRoute(t *testing.T) {
	route := orderedRoute(1, 2, 3, 4, 5, 6)
	route.Metrics = Metrics{TotalMinutes: 190, WalkKM: 4.24, CostMinor: 1850, CategoryCounts: map[string]int{"museums": 2, "monuments": 3, "parks": 1}}
	want := "6 мест, до 3 ч 10 мин, 4,2 км пешком. По категориям: 3 памятника, 2 музея, 1 парк. Билеты: 18,50 BYN. По расписанию все места открыты на время осмотра."
	if got := Explain(route); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	for i := 0; i < 10; i++ {
		if got := Explain(route); got != want {
			t.Fatal("map order changed explanation")
		}
	}
}
func TestExplainUnknownFactsAndOverlappingCategories(t *testing.T) {
	route := orderedRoute(1, 2)
	route.Stops[0].PriceUnconfirmed = true
	route.Stops[1].HoursUnconfirmed = true
	route.Metrics = Metrics{TotalMinutes: 60.01, WalkKM: 0.5, CostMinor: 0, CategoryCounts: map[string]int{"museums": 2, "architecture": 2}}
	got := Explain(route)
	for _, want := range []string{"2 места, до 1 ч 1 мин", "Категории могут пересекаться", "Подтверждённая стоимость билетов: 0 BYN", "цены 1 места не подтверждены", "Часы работы 1 места не подтверждены"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "Билеты бесплатные") || strings.Contains(got, "все места открыты") {
		t.Fatal("unknown facts presented as verified")
	}
}
func TestExplainEmptyAndRussianPlural(t *testing.T) {
	if got := Explain(Route{}); got != "Маршрут пуст: подходящих мест нет." {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		n    int
		word string
	}{{1, "место"}, {2, "места"}, {5, "мест"}, {11, "мест"}, {14, "мест"}, {21, "место"}, {22, "места"}, {111, "мест"}} {
		if got := plural(tc.n, "место", "места", "мест"); got != tc.word {
			t.Errorf("%d: %s", tc.n, got)
		}
	}
	route := orderedRoute(1)
	route.Metrics = Metrics{TotalMinutes: 30, CostMinor: 0}
	if got := Explain(route); !strings.Contains(got, "Билеты бесплатные") {
		t.Fatal(got)
	}
}
func TestRouteDiffDeltasSetsAndText(t *testing.T) {
	before, after := orderedRoute(1, 2, 3), orderedRoute(3, 4, 5)
	before.Metrics = Metrics{TotalMinutes: 180, WalkKM: 4, CostMinor: 1800}
	after.Metrics = Metrics{TotalMinutes: 155.5, WalkKM: 3.25, CostMinor: 1250}
	diff := DiffRoutes(before, after)
	want := RouteDiff{Minutes: -24.5, DistanceKM: -0.75, CostMinor: -550, AddedPlaceIDs: []int64{4, 5}, RemovedPlaceIDs: []int64{1, 2}, PricesConfirmed: true}
	if !reflect.DeepEqual(diff, want) {
		t.Fatalf("got=%+v want=%+v", diff, want)
	}
	text := ExplainDiff(diff, map[int64]string{4: "Парк", 1: "Музей"})
	for _, expected := range []string{"время меньше на 24,50 мин", "расстояние меньше на 0,75 км пешком", "билеты дешевле на 5,50 BYN", "Добавлены: Парк, ID 5.", "Убраны: Музей, ID 2."} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
	reverse := DiffRoutes(after, before)
	if reverse.Minutes != 24.5 || reverse.DistanceKM != 0.75 || reverse.CostMinor != 550 {
		t.Fatal("wrong delta direction")
	}
	if text := ExplainDiff(reverse, nil); !strings.Contains(text, "дороже на 5,50 BYN") {
		t.Fatal(text)
	}
}
func TestDiffOrderOnlyAndUnknownPrices(t *testing.T) {
	before, after := orderedRoute(1, 2, 3), orderedRoute(3, 2, 1)
	diff := DiffRoutes(before, after)
	if len(diff.AddedPlaceIDs) != 0 || len(diff.RemovedPlaceIDs) != 0 {
		t.Fatal("reordering changed set")
	}
	got := ExplainDiff(diff, nil)
	if got != "время без изменений; расстояние без изменений; стоимость билетов без изменений. Набор мест не изменился." {
		t.Fatal(got)
	}
	after.Stops[0].PriceUnconfirmed = true
	diff = DiffRoutes(before, after)
	if diff.PricesConfirmed {
		t.Fatal("unconfirmed price ignored")
	}
	if got := ExplainDiff(diff, nil); !strings.Contains(got, "подтверждённая стоимость билетов") || !strings.Contains(got, "Неизвестные цены не включены") {
		t.Fatal(got)
	}
	diff.CostMinor = 150
	if got := ExplainDiff(diff, nil); !strings.Contains(got, "подтверждённая стоимость билетов больше на 1,50 BYN") {
		t.Fatal(got)
	}
}
func TestDiffEmptyRoutesAndSmallChange(t *testing.T) {
	diff := DiffRoutes(Route{}, orderedRoute(3, 2))
	if !reflect.DeepEqual(diff.AddedPlaceIDs, []int64{3, 2}) || len(diff.RemovedPlaceIDs) != 0 {
		t.Fatalf("wrong additions %+v", diff)
	}
	removed := DiffRoutes(orderedRoute(3, 2), Route{})
	if !reflect.DeepEqual(removed.RemovedPlaceIDs, []int64{3, 2}) {
		t.Fatal(removed)
	}
	text := ExplainDiff(RouteDiff{Minutes: 0.001, DistanceKM: -0.001, PricesConfirmed: true}, nil)
	if !strings.Contains(text, "время больше на <0,01 мин") || !strings.Contains(text, "расстояние меньше на <0,01 км") {
		t.Fatal(text)
	}
}
