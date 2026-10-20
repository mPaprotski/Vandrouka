package apihttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"io"
	"math"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // The minimal API image has no system timezone database.
	"unicode/utf8"
)

const MaxRouteRequestBytes = 64 << 10
const MaxRouteSeed int64 = 9007199254740991

var ErrInvalidRouteRequest = errors.New("invalid route request")
var routeDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[Zz]|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

// ValidatedGenerateRouteRequest has explicit defaults, while a nil ticket budget
// still means unlimited. It is not a Planner request and causes no service calls.
type ValidatedGenerateRouteRequest struct {
	City              string
	Start             Coordinates
	StartAt           time.Time
	BudgetMinutes     int
	Pace              string
	Categories        []string
	TicketBudgetMinor *int64
	Seed              int64
	IncludeGeometry   bool
}

// ReadGenerateRouteRequest is the HTTP boundary for the future route handler.
// A failed read writes the existing API error envelope and stops processing.
func ReadGenerateRouteRequest(w http.ResponseWriter, r *http.Request) (ValidatedGenerateRouteRequest, bool) {
	requestlog.SetStage(r.Context(), "validation")
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		Error(w, http.StatusBadRequest, "invalid_request", "Ожидается Content-Type application/json")
		return ValidatedGenerateRouteRequest{}, false
	}
	if r.Body == nil {
		r.Body = http.NoBody
	}
	defer r.Body.Close()
	request, err := ParseGenerateRouteRequest(r.Body)
	if err != nil {
		if errors.Is(err, ErrInvalidRouteRequest) {
			message := strings.TrimPrefix(err.Error(), ErrInvalidRouteRequest.Error()+": ")
			Error(w, http.StatusBadRequest, "invalid_request", message)
		} else {
			failure(w, err)
		}
		return ValidatedGenerateRouteRequest{}, false
	}
	return request, true
}

// InCityTimezone is used after the repository resolves a city's metadata.
// Unknown but syntactically valid city slugs remain eligible for a later 404.
func (r ValidatedGenerateRouteRequest) InCityTimezone(name string) (ValidatedGenerateRouteRequest, error) {
	location, err := time.LoadLocation(name)
	if err != nil {
		return r, fmt.Errorf("load city timezone: %w", err)
	}
	r.StartAt = r.StartAt.In(location)
	return r, nil
}

// ParseGenerateRouteRequest rejects ambiguous JSON, unknown fields and invalid
// values before accessing a repository or router. The supported Minsk region
// uses the same bounding rectangle as the catalog importer, not a city polygon.
func ParseGenerateRouteRequest(reader io.Reader) (ValidatedGenerateRouteRequest, error) {
	body, err := io.ReadAll(io.LimitReader(reader, MaxRouteRequestBytes+1))
	if err != nil {
		return ValidatedGenerateRouteRequest{}, invalidRoute("Не удалось прочитать JSON")
	}
	if len(body) > MaxRouteRequestBytes {
		return ValidatedGenerateRouteRequest{}, invalidRoute("Тело запроса превышает 64 КиБ")
	}
	if !utf8.Valid(body) {
		return ValidatedGenerateRouteRequest{}, invalidRoute("JSON должен быть в UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := checkUniqueJSON(decoder); err != nil {
		return ValidatedGenerateRouteRequest{}, invalidRoute("Некорректный JSON или повторные поля")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ValidatedGenerateRouteRequest{}, invalidRoute("Ожидается один JSON-документ")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return ValidatedGenerateRouteRequest{}, invalidRoute("Ожидается JSON-объект")
	}
	allowed := map[string]bool{"city": true, "start": true, "start_at": true, "budget_minutes": true, "pace": true, "categories": true, "ticket_budget_minor": true, "seed": true, "include_geometry": true}
	for key, value := range fields {
		if !allowed[key] {
			return ValidatedGenerateRouteRequest{}, invalidRoute("Неизвестное поле запроса")
		}
		if key != "ticket_budget_minor" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ValidatedGenerateRouteRequest{}, invalidRoute("null допустим только для ticket_budget_minor")
		}
	}
	var start map[string]json.RawMessage
	if err := json.Unmarshal(fields["start"], &start); err != nil || len(start) != 2 || start["lat"] == nil || start["lon"] == nil {
		return ValidatedGenerateRouteRequest{}, invalidRoute("start должен содержать lat и lon")
	}
	for _, value := range start {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ValidatedGenerateRouteRequest{}, invalidRoute("Координаты не могут быть null")
		}
	}
	var timestamp string
	if err := json.Unmarshal(fields["start_at"], &timestamp); err != nil || !routeDateTime.MatchString(timestamp) {
		return ValidatedGenerateRouteRequest{}, invalidRoute("start_at должен быть RFC3339 с часовым смещением")
	}
	// RFC3339 permits lowercase t/z; time.Time's decoder expects uppercase.
	timestamp = strings.ReplaceAll(strings.ReplaceAll(timestamp, "t", "T"), "z", "Z")
	fields["start_at"], _ = json.Marshal(timestamp)
	normalized, err := json.Marshal(fields)
	if err != nil {
		return ValidatedGenerateRouteRequest{}, invalidRoute("Некорректный JSON")
	}
	var request GenerateRouteRequest
	decoder = json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return ValidatedGenerateRouteRequest{}, invalidRoute("Неверный тип поля или значение даты")
	}
	return ValidateGenerateRouteRequest(request)
}

func ValidateGenerateRouteRequest(request GenerateRouteRequest) (ValidatedGenerateRouteRequest, error) {
	result := ValidatedGenerateRouteRequest{City: "minsk", Pace: "normal", Seed: 42, IncludeGeometry: true, Categories: []string{}}
	if request.City != nil {
		result.City = *request.City
	}
	if len(result.City) > 100 || !validSlug.MatchString(result.City) {
		return result, invalidRoute("Некорректный идентификатор города")
	}
	if request.Start == nil || request.StartAt == nil || request.StartAt.IsZero() || request.BudgetMinutes == nil {
		return result, invalidRoute("Обязательны start, start_at и budget_minutes")
	}
	result.Start = *request.Start
	result.StartAt = *request.StartAt
	result.BudgetMinutes = *request.BudgetMinutes
	if !finiteCoordinate(result.Start.Lat) || !finiteCoordinate(result.Start.Lon) || result.Start.Lat < -90 || result.Start.Lat > 90 || result.Start.Lon < -180 || result.Start.Lon > 180 {
		return result, invalidRoute("Некорректные координаты")
	}
	if result.City == "minsk" && (result.Start.Lat < 53.75 || result.Start.Lat > 54.05 || result.Start.Lon < 27.3 || result.Start.Lon > 27.85) {
		return result, invalidRoute("Старт вне поддерживаемой области Минска")
	}
	if result.BudgetMinutes < 1 || result.BudgetMinutes > 1440 {
		return result, invalidRoute("Бюджет времени должен быть от 1 до 1440 минут")
	}
	if request.Pace != nil {
		result.Pace = *request.Pace
	}
	switch result.Pace {
	case "slow", "normal", "fast":
	default:
		return result, invalidRoute("Неизвестный темп ходьбы")
	}
	allowed := map[string]bool{"museums": true, "parks": true, "architecture": true, "religious": true, "monuments": true, "streets": true}
	seen := map[string]bool{}
	for _, category := range request.Categories {
		if !allowed[category] || seen[category] {
			return result, invalidRoute("Неизвестная или повторная категория")
		}
		seen[category] = true
		result.Categories = append(result.Categories, category)
	}
	if request.TicketBudgetMinor != nil {
		if *request.TicketBudgetMinor < 0 {
			return result, invalidRoute("Бюджет билетов не может быть отрицательным")
		}
		cost := *request.TicketBudgetMinor
		result.TicketBudgetMinor = &cost
	}
	if request.Seed != nil {
		result.Seed = *request.Seed
	}
	if result.Seed < 0 || result.Seed > MaxRouteSeed {
		return result, invalidRoute("Некорректный seed")
	}
	if request.IncludeGeometry != nil {
		result.IncludeGeometry = *request.IncludeGeometry
	}
	if result.City == "minsk" {
		var err error
		result, err = result.InCityTimezone("Europe/Minsk")
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
func invalidRoute(message string) error   { return fmt.Errorf("%w: %s", ErrInvalidRouteRequest, message) }
func finiteCoordinate(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// checkUniqueJSON walks nested containers; encoding/json otherwise silently
// accepts the last value of a duplicate key.
func checkUniqueJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrInvalidRouteRequest
			}
			seen[name] = true
			if err := checkUniqueJSON(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := checkUniqueJSON(decoder); err != nil {
				return err
			}
		}
	default:
		return ErrInvalidRouteRequest
	}
	_, err = decoder.Token()
	return err
}
