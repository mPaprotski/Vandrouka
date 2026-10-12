package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yourname/vandrouka/backend/internal/config"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routing"
	"github.com/yourname/vandrouka/backend/internal/store"
)

// Default start location: Minsk City Hall (Площадь Свободы / Ратуша)
const (
	defaultStartLat = 53.9032997
	defaultStartLon = 27.5561469
	minskTimezone   = "Europe/Minsk"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("planner", flag.ContinueOnError)

	budget := fs.Int("budget", 180, "Бюджет времени маршрута в минутах (по умолчанию: 180)")
	categoriesStr := fs.String("categories", "", "Список категорий через запятую (например: museums,architecture). По умолчанию: все категории")
	startCoords := fs.String("start", fmt.Sprintf("%.7f,%.7f", defaultStartLat, defaultStartLon), "Координаты старта 'широта,долгота' (по умолчанию: Минская ратуша)")
	startAtStr := fs.String("start-time", "", "Время начала 'YYYY-MM-DDTHH:MM' или 'HH:MM' (по умолчанию: текущее время по Минску)")
	paceStr := fs.String("pace", "normal", "Темп ходьбы: slow, normal, fast (по умолчанию: normal)")
	ticketBudget := fs.Float64("ticket-budget", -1, "Максимальный бюджет на билеты в BYN (по умолчанию: без ограничений)")
	seed := fs.Int64("seed", 42, "Случайное зерно (seed) для детерминированного выбора (по умолчанию: 42)")
	variants := fs.Bool("variants", true, "Показать до трёх разных вариантов маршрута")
	geometry := fs.Bool("geometry", true, "Проверить итоговый путь и получить геометрию через Mapbox Directions")
	profile := fs.String("profile", "best_match", "Профиль одного маршрута: best_match, more_places, less_walking, budget")
	optimize := fs.Bool("optimize", true, "Уменьшить время ходьбы с помощью 2-opt и swap")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *budget <= 0 {
		return fmt.Errorf("бюджет времени должен быть положительным числом минут (--budget %d)", *budget)
	}

	switch planner.Profile(*profile) {
	case planner.ProfileBestMatch, planner.ProfileMorePlaces, planner.ProfileLessWalking, planner.ProfileBudget:
	default:
		return fmt.Errorf("неизвестный профиль %q", *profile)
	}
	loc, err := time.LoadLocation(minskTimezone)
	if err != nil {
		return fmt.Errorf("не удалось загрузить часовой пояс %s: %w", minskTimezone, err)
	}

	startPoint, err := parseCoordinates(*startCoords)
	if err != nil {
		return fmt.Errorf("некорректные координаты старта %q: %w", *startCoords, err)
	}

	startTime, err := parseStartTime(*startAtStr, loc)
	if err != nil {
		return fmt.Errorf("некорректное время начала %q: %w", *startAtStr, err)
	}

	pace := planner.Pace(strings.ToLower(strings.TrimSpace(*paceStr)))
	switch pace {
	case planner.PaceSlow, planner.PaceNormal, planner.PaceFast:
	default:
		return fmt.Errorf("неизвестный темп ходьбы %q (допустимы: slow, normal, fast)", *paceStr)
	}

	var maxTicketMinor *int64
	if math.IsNaN(*ticketBudget) || math.IsInf(*ticketBudget, 0) || (*ticketBudget < 0 && *ticketBudget != -1) || *ticketBudget*100 >= float64(math.MaxInt64) {
		return fmt.Errorf("бюджет билетов должен быть конечным неотрицательным числом BYN или -1")
	}
	if *ticketBudget >= 0 {
		minor := int64(math.Round(*ticketBudget * 100))
		maxTicketMinor = &minor
	}

	var categories []string
	if strings.TrimSpace(*categoriesStr) != "" {
		for _, cat := range strings.Split(*categoriesStr, ",") {
			c := strings.TrimSpace(cat)
			if c != "" {
				categories = append(categories, c)
			}
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("ошибка конфигурации: %w. Убедитесь, что переменная POSTGRES_DSN задана", err)
	}

	if strings.TrimSpace(cfg.MapboxAccessToken) == "" {
		return fmt.Errorf("задайте MAPBOX_ACCESS_TOKEN для маршрутизации Mapbox")
	}

	pln, err := planner.NewWithWeights(planner.ScoringWeights{WalkKM: cfg.PlannerWalkPenalty, CostBYN: cfg.PlannerCostPenalty, Unverified: cfg.PlannerUnverifiedPenalty})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := store.Open(ctx, cfg.PostgresDSN)
	if err != nil {
		return fmt.Errorf("не удалось подключиться к базе данных PostgreSQL: %w", err)
	}
	defer pool.Close()

	catalog := store.NewCatalog(pool)
	catalogPlaces, err := catalog.ListPlaces(ctx, "minsk", "", "")
	if err != nil {
		return fmt.Errorf("ошибка загрузки каталога мест из базы данных: %w", err)
	}
	if len(catalogPlaces) == 0 {
		return fmt.Errorf("в каталоге Минска нет активных мест. Запустите импорт данных (make import)")
	}

	plannerPlaces := make([]planner.Place, len(catalogPlaces))
	for i, cp := range catalogPlaces {
		plannerPlaces[i] = planner.FromCatalogPlace(cp)
	}

	routingClient := routing.NewMapboxClient(cfg.MapboxURL, cfg.MapboxAccessToken, 15*time.Second)
	matrixBuilder := planner.NewRouterMatrixBuilder(routingClient)

	matrix, err := matrixBuilder.BuildMatrix(ctx, startPoint, plannerPlaces)
	if err != nil {
		if errors.Is(err, routing.ErrUnavailable) {
			return fmt.Errorf("сервис маршрутизации Mapbox недоступен: %w", err)
		}
		return fmt.Errorf("ошибка построения матрицы расстояний Mapbox: %w", err)
	}

	req := planner.Request{
		Categories:         categories,
		BudgetMinutes:      *budget,
		Start:              startPoint,
		StartAt:            startTime,
		Pace:               pace,
		MaxTicketCostMinor: maxTicketMinor,
		Seed:               *seed,
		Profile:            planner.Profile(*profile),
	}

	var results []planner.Variant
	if *variants {
		results, err = pln.PlanVariants(req, plannerPlaces, matrix, *optimize)
	} else {
		var route planner.Route
		route, err = pln.Plan(req, plannerPlaces, matrix)
		if err == nil {
			before := route.Metrics.WalkMinutes
			if *optimize {
				route, err = pln.Optimize(req, plannerPlaces, matrix, route)
			}
			results = []planner.Variant{{Profile: req.Profile, Route: route, BeforeWalkMinutes: before}}
		}
	}
	if err != nil {
		return fmt.Errorf("ошибка планирования маршрутов: %w", err)
	}
	if len(results) == 0 {
		fmt.Println("Маршруты не найдены для выбранных ограничений.")
		return nil
	}
	type finalVariant struct {
		variant    planner.Variant
		matrix     planner.DistanceMatrix
		shapes     []string
		matrixWalk float64
	}
	var finals []finalVariant
	for _, variant := range results {
		route := variant.Route
		var shapes []string
		displayMatrix := matrix
		if *geometry && len(route.Stops) > 0 {
			points := []routing.Point{{Lat: req.Start.Lat, Lon: req.Start.Lon}}
			order := make([]int64, len(route.Stops))
			indices := make(map[int64]int, len(plannerPlaces))
			for i, place := range plannerPlaces {
				if _, ok := indices[place.ID]; !ok {
					indices[place.ID] = i
				}
			}
			for i, stop := range route.Stops {
				place := plannerPlaces[indices[stop.PlaceID]]
				points = append(points, routing.Point{Lat: place.Location.Lat, Lon: place.Location.Lon})
				order[i] = stop.PlaceID
			}
			path, pathErr := routingClient.Route(ctx, points)
			if errors.Is(pathErr, routing.ErrNoRoute) {
				fmt.Fprintln(os.Stderr, "Вариант пропущен: Mapbox Directions не нашёл путь.")
				continue
			}
			if pathErr != nil {
				return fmt.Errorf("ошибка Mapbox Directions: %w", pathErr)
			}
			confirmed := routeMatrix{base: matrix, legs: make(map[[2]int]routing.Leg)}
			from := 0
			for i, id := range order {
				to := indices[id] + 1
				confirmed.legs[[2]int{from, to}] = path.Legs[i]
				from = to
			}
			verifiedReq := req
			verifiedReq.Profile = variant.Profile
			route, pathErr = pln.EvaluateOrder(verifiedReq, plannerPlaces, confirmed, order)
			if errors.Is(pathErr, planner.ErrInfeasible) {
				fmt.Fprintln(os.Stderr, "Вариант пропущен: уточнённый путь Directions нарушает ограничения времени или расписания.")
				continue
			}
			if pathErr != nil {
				return pathErr
			}
			displayMatrix = confirmed
			shapes = path.Shapes
		}
		matrixWalk := variant.Route.Metrics.WalkMinutes
		variant.Route = route
		finals = append(finals, finalVariant{variant: variant, matrix: displayMatrix, shapes: shapes, matrixWalk: matrixWalk})
	}
	if len(finals) == 0 {
		return fmt.Errorf("ни один вариант не прошёл уточнение пути Mapbox Directions")
	}
	finalVariants := make([]planner.Variant, len(finals))
	for i, f := range finals {
		finalVariants[i] = f.variant
	}
	labeled := planner.AssignLabels(finalVariants)
	placeNames := make(map[int64]string, len(plannerPlaces))
	for _, place := range plannerPlaces {
		if _, exists := placeNames[place.ID]; !exists {
			placeNames[place.ID] = place.Name
		}
	}
	for i, f := range finals {
		variant := labeled[i]
		fmt.Printf("Вариант %d (профиль: %s)\n", i+1, variant.Profile)
		if len(variant.Labels) > 0 {
			names := make([]string, len(variant.Labels))
			for j, label := range variant.Labels {
				names[j] = string(label)
			}
			fmt.Printf("Метки: %s\n", strings.Join(names, ", "))
		}
		fmt.Println(planner.Explain(variant.Route))
		if i > 0 {
			fmt.Printf("По сравнению с вариантом 1: %s\n", planner.ExplainDiff(planner.DiffRoutes(labeled[0].Route, variant.Route), placeNames))
		}
		printRouteResult(req, variant.Route, plannerPlaces, f.matrix, pace)
		if *optimize {
			fmt.Printf("Ходьба по Matrix: до оптимизации %.2f мин; после %.2f мин; экономия %.2f мин\n", variant.BeforeWalkMinutes, f.matrixWalk, variant.BeforeWalkMinutes-f.matrixWalk)
		}
		if *geometry {
			fmt.Printf("Итоговые переходы и геометрия проверены через Directions (%d частей пути).\n", len(f.shapes))
		}
	}

	return nil
}

func parseCoordinates(s string) (planner.Point, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return planner.Point{}, fmt.Errorf("ожидается формат 'широта,долгота'")
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return planner.Point{}, fmt.Errorf("недопустимые значения координат")
	}
	return planner.Point{Lat: lat, Lon: lon}, nil
}

func parseStartTime(s string, loc *time.Location) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		now := time.Now().In(loc)
		return now.Truncate(time.Minute), nil
	}
	s = strings.TrimSpace(s)
	// Try full format: 2006-01-02T15:04 or 2006-01-02 15:04
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	// Try time-only format: 15:04
	if t, err := time.ParseInLocation("15:04", s, loc); err == nil {
		now := time.Now().In(loc)
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
	}
	return time.Time{}, fmt.Errorf("ожидается 'YYYY-MM-DDTHH:MM' или 'HH:MM'")
}

func printRouteResult(req planner.Request, route planner.Route, places []planner.Place, matrix planner.DistanceMatrix, pace planner.Pace) {
	placesMap := make(map[int64]planner.Place, len(places))
	placeIdxMap := make(map[int64]int, len(places))
	for idx, p := range places {
		placesMap[p.ID] = p
		placeIdxMap[p.ID] = idx
	}

	fmt.Printf("\n=== Вандроўка: Пешеходный маршрут по Минску ===\n\n")
	fmt.Printf("Параметры запроса:\n")
	fmt.Printf("  Бюджет времени: %d мин\n", req.BudgetMinutes)
	if len(req.Categories) > 0 {
		fmt.Printf("  Категории:      %s\n", strings.Join(req.Categories, ", "))
	} else {
		fmt.Printf("  Категории:      все категории\n")
	}
	fmt.Printf("  Старт:          %.6f, %.6f\n", req.Start.Lat, req.Start.Lon)
	fmt.Printf("  Время начала:   %s\n", req.StartAt.Format("2006-01-02 15:04"))
	fmt.Printf("  Темп:           %s (x%.2f)\n", req.Pace, pace.WalkMultiplier())
	if req.MaxTicketCostMinor != nil {
		fmt.Printf("  Бюджет билетов: %.2f BYN\n", float64(*req.MaxTicketCostMinor)/100.0)
	} else {
		fmt.Printf("  Бюджет билетов: не ограничен\n")
	}
	fmt.Printf("  Seed:           %d\n\n", req.Seed)

	if len(route.Stops) == 0 {
		fmt.Println("Маршрут пуст: ни одно место не подошло под выбранные критерии или ограничения времени/расписания.")
		return
	}

	fmt.Println("План остановок:")
	fmt.Println("--------------------------------------------------------------------------------")

	currentLocIdx := 0
	paceMult := pace.WalkMultiplier()

	for i, stop := range route.Stops {
		pl := placesMap[stop.PlaceID]
		targetIdx := 1 + placeIdxMap[stop.PlaceID]
		leg := matrix.Leg(currentLocIdx, targetIdx)
		walkMin := (leg.Seconds / 60.0) * paceMult

		fmt.Printf("%d. %s\n", i+1, pl.Name)
		fmt.Printf("   Прибытие: %s | Уход: %s\n", stop.Arrival.Format("15:04"), stop.Departure.Format("15:04"))
		fmt.Printf("   Переход:  %4.1f мин (%.2f км) | Осмотр: %d мин\n", walkMin, leg.Kilometers, pl.VisitMinutes)

		if pl.PriceKnown {
			fmt.Printf("   Билет:    %.2f BYN\n", float64(pl.TicketCostMinor)/100.0)
		} else {
			fmt.Printf("   Билет:    не подтверждён [!]\n")
		}

		var warnings []string
		if stop.HoursUnconfirmed {
			warnings = append(warnings, "часы работы не подтверждены")
		}
		if stop.PriceUnconfirmed {
			warnings = append(warnings, "цена билета не подтверждена")
		}
		if len(warnings) > 0 {
			fmt.Printf("   Предупреждение: %s\n", strings.Join(warnings, "; "))
		}
		fmt.Println()

		currentLocIdx = targetIdx
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Итоговые показатели:\n")
	fmt.Printf("  Количество остановок:     %d\n", len(route.Stops))
	fmt.Printf("  Пешком:                   %.1f мин (%.2f км)\n", route.Metrics.WalkMinutes, route.Metrics.WalkKM)
	fmt.Printf("  Осмотр:                   %d мин\n", route.Metrics.VisitMinutes)
	fmt.Printf("  Общее время:              %.1f мин (%.1f ч)\n", route.Metrics.TotalMinutes, route.Metrics.TotalMinutes/60.0)
	fmt.Printf("  Общая стоимость билетов:  %.2f BYN\n", float64(route.Metrics.CostMinor)/100.0)
	fmt.Printf("  Неподтверждённых данных:  %d остановок\n", route.Metrics.UnverifiedCount)

	if len(route.Metrics.CategoryCounts) > 0 {
		var cats []string
		for cat, count := range route.Metrics.CategoryCounts {
			cats = append(cats, fmt.Sprintf("%s: %d", cat, count))
		}
		sort.Strings(cats)
		fmt.Printf("  Категории:                %s\n", strings.Join(cats, ", "))
	}
	fmt.Printf("  Оценка маршрута (score):  %.2f\n\n", route.Metrics.Score)
}

// routeMatrix replaces only the selected path edges with Directions durations.
// Other pairs retain Matrix estimates; planner applies the user's pace once.
type routeMatrix struct {
	base planner.DistanceMatrix
	legs map[[2]int]routing.Leg
}

func (m routeMatrix) Leg(from, to int) routing.Leg {
	if leg, ok := m.legs[[2]int{from, to}]; ok {
		return leg
	}
	return m.base.Leg(from, to)
}
