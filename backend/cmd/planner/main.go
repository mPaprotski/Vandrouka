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

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *budget <= 0 {
		return fmt.Errorf("бюджет времени должен быть положительным числом минут (--budget %d)", *budget)
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	routingClient := routing.NewClient(cfg.ValhallaURL, 15*time.Second)
	matrixBuilder := planner.NewRouterMatrixBuilder(routingClient)

	matrix, err := matrixBuilder.BuildMatrix(ctx, startPoint, plannerPlaces)
	if err != nil {
		if errors.Is(err, routing.ErrUnavailable) {
			return fmt.Errorf("сервис маршрутизации Valhalla недоступен (%s): %w", cfg.ValhallaURL, err)
		}
		return fmt.Errorf("ошибка построения матрицы расстояний Valhalla: %w", err)
	}

	req := planner.Request{
		Categories:         categories,
		BudgetMinutes:      *budget,
		Start:              startPoint,
		StartAt:            startTime,
		Pace:               pace,
		MaxTicketCostMinor: maxTicketMinor,
		Seed:               *seed,
	}

	pln := planner.New()
	route, err := pln.Plan(req, plannerPlaces, matrix)
	if err != nil {
		return fmt.Errorf("ошибка планирования маршрута: %w", err)
	}

	printRouteResult(req, route, plannerPlaces, matrix, pace)
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
