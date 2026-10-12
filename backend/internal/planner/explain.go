package planner

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Explain summarizes a final evaluated route. Durations round up to a minute;
// exact budgets continue to be checked using durations, not this presentation.
func Explain(route Route) string {
	if len(route.Stops) == 0 {
		return "Маршрут пуст: подходящих мест нет."
	}
	text := fmt.Sprintf("%d %s, до %s, %s км пешком.", len(route.Stops), plural(len(route.Stops), "место", "места", "мест"), formatMinutes(route.Metrics.TotalMinutes), decimal(route.Metrics.WalkKM, 1))
	keys := make([]string, 0, len(route.Metrics.CategoryCounts))
	for category, count := range route.Metrics.CategoryCounts {
		if count > 0 {
			keys = append(keys, category)
		}
	}
	sort.Strings(keys)
	categories := make([]string, 0, len(keys))
	sum := 0
	for _, category := range keys {
		count := route.Metrics.CategoryCounts[category]
		sum += count
		names := categoryForms(category)
		categories = append(categories, fmt.Sprintf("%d %s", count, plural(count, names[0], names[1], names[2])))
	}
	if len(categories) > 0 {
		text += " По категориям: " + strings.Join(categories, ", ") + "."
		if sum > len(route.Stops) {
			text += " Категории могут пересекаться."
		}
	}
	hours, prices := unconfirmed(route)
	if prices > 0 {
		text += fmt.Sprintf(" Подтверждённая стоимость билетов: %s BYN; цены %d %s не подтверждены.", money(route.Metrics.CostMinor), prices, plural(prices, "места", "мест", "мест"))
	} else if route.Metrics.CostMinor == 0 {
		text += " Билеты бесплатные."
	} else {
		text += " Билеты: " + money(route.Metrics.CostMinor) + " BYN."
	}
	if hours > 0 {
		text += fmt.Sprintf(" Часы работы %d %s не подтверждены.", hours, plural(hours, "места", "мест", "мест"))
	} else {
		text += " По расписанию все места открыты на время осмотра."
	}
	return text
}
func unconfirmed(route Route) (hours, prices int) {
	for _, stop := range route.Stops {
		if stop.HoursUnconfirmed {
			hours++
		}
		if stop.PriceUnconfirmed {
			prices++
		}
	}
	return
}
func plural(n int, one, few, many string) string {
	if n%100 >= 11 && n%100 <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}
func categoryForms(category string) [3]string {
	switch category {
	case "museums":
		return [3]string{"музей", "музея", "музеев"}
	case "monuments":
		return [3]string{"памятник", "памятника", "памятников"}
	case "parks":
		return [3]string{"парк", "парка", "парков"}
	case "architecture":
		return [3]string{"объект архитектуры", "объекта архитектуры", "объектов архитектуры"}
	case "religious":
		return [3]string{"религиозный объект", "религиозных объекта", "религиозных объектов"}
	case "streets":
		return [3]string{"улица или квартал", "улицы или квартала", "улиц или кварталов"}
	default:
		return [3]string{"объект категории «" + category + "»", "объекта категории «" + category + "»", "объектов категории «" + category + "»"}
	}
}
func decimal(n float64, digits int) string {
	return strings.ReplaceAll(fmt.Sprintf("%.*f", digits, n), ".", ",")
}
func money(minor int64) string {
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	if minor%100 == 0 {
		return fmt.Sprintf("%s%d", sign, minor/100)
	}
	return fmt.Sprintf("%s%d,%02d", sign, minor/100, minor%100)
}
func formatMinutes(minutes float64) string {
	n := int(math.Ceil(math.Abs(minutes)))
	if n < 60 {
		return fmt.Sprintf("%d мин", n)
	}
	if n%60 == 0 {
		return fmt.Sprintf("%d ч", n/60)
	}
	return fmt.Sprintf("%d ч %d мин", n/60, n%60)
}

// RouteDiff stores signed after-minus-before deltas and set changes. IDs follow
// the respective routes' order; an order-only change does not add/remove places.
type RouteDiff struct {
	Minutes         float64
	DistanceKM      float64
	CostMinor       int64
	AddedPlaceIDs   []int64
	RemovedPlaceIDs []int64
	PricesConfirmed bool
}

func DiffRoutes(before, after Route) RouteDiff {
	_, beforeUnknown := unconfirmed(before)
	_, afterUnknown := unconfirmed(after)
	diff := RouteDiff{Minutes: after.Metrics.TotalMinutes - before.Metrics.TotalMinutes, DistanceKM: after.Metrics.WalkKM - before.Metrics.WalkKM, CostMinor: after.Metrics.CostMinor - before.Metrics.CostMinor, PricesConfirmed: beforeUnknown == 0 && afterUnknown == 0}
	beforeSet := make(map[int64]bool)
	afterSet := make(map[int64]bool)
	for _, stop := range before.Stops {
		beforeSet[stop.PlaceID] = true
	}
	for _, stop := range after.Stops {
		afterSet[stop.PlaceID] = true
	}
	added := make(map[int64]bool)
	removed := make(map[int64]bool)
	for _, stop := range after.Stops {
		if !beforeSet[stop.PlaceID] && !added[stop.PlaceID] {
			diff.AddedPlaceIDs = append(diff.AddedPlaceIDs, stop.PlaceID)
			added[stop.PlaceID] = true
		}
	}
	for _, stop := range before.Stops {
		if !afterSet[stop.PlaceID] && !removed[stop.PlaceID] {
			diff.RemovedPlaceIDs = append(diff.RemovedPlaceIDs, stop.PlaceID)
			removed[stop.PlaceID] = true
		}
	}
	return diff
}

// ExplainDiff renders comparisons and can also be reused after recalculation.
// placeNames may be nil; absent names are shown as IDs, never silently omitted.
func ExplainDiff(diff RouteDiff, placeNames map[int64]string) string {
	parts := []string{floatChange(diff.Minutes, "мин"), floatChange(diff.DistanceKM, "км пешком")}
	if diff.CostMinor == 0 {
		if diff.PricesConfirmed {
			parts = append(parts, "стоимость билетов без изменений")
		} else {
			parts = append(parts, "подтверждённая стоимость билетов без изменений")
		}
	} else {
		direction := "дешевле"
		amount := diff.CostMinor
		if amount > 0 {
			direction = "дороже"
		} else {
			amount = -amount
		}
		prefix := "билеты"
		if !diff.PricesConfirmed {
			prefix = "подтверждённая стоимость билетов"
			if diff.CostMinor > 0 {
				direction = "больше"
			} else {
				direction = "меньше"
			}
		}
		parts = append(parts, fmt.Sprintf("%s %s на %s BYN", prefix, direction, money(amount)))
	}
	text := strings.Join(parts, "; ") + "."
	names := func(ids []int64) string {
		items := make([]string, len(ids))
		for i, id := range ids {
			if name := placeNames[id]; name != "" {
				items[i] = name
			} else {
				items[i] = fmt.Sprintf("ID %d", id)
			}
		}
		return strings.Join(items, ", ")
	}
	if len(diff.AddedPlaceIDs) > 0 {
		text += " Добавлены: " + names(diff.AddedPlaceIDs) + "."
	}
	if len(diff.RemovedPlaceIDs) > 0 {
		text += " Убраны: " + names(diff.RemovedPlaceIDs) + "."
	}
	if len(diff.AddedPlaceIDs) == 0 && len(diff.RemovedPlaceIDs) == 0 {
		text += " Набор мест не изменился."
	}
	if !diff.PricesConfirmed {
		text += " Неизвестные цены не включены в сравнение."
	}
	return text
}
func floatChange(delta float64, unit string) string {
	noun := "время"
	if unit == "км пешком" {
		noun = "расстояние"
	}
	if delta == 0 {
		return noun + " без изменений"
	}
	direction := "меньше"
	if delta > 0 {
		direction = "больше"
	}
	// Differences use decimals instead of rounding to a whole minute to retain
	// small changes. Tiny nonzero deltas are explicitly marked as <0.01.
	value := decimal(math.Abs(delta), 2)
	if math.Abs(delta) < 0.005 {
		value = "<0,01"
	}
	return fmt.Sprintf("%s %s на %s %s", noun, direction, value, unit)
}
