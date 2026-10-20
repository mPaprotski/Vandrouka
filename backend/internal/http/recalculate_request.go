package apihttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

// ValidatedRecalculateRouteRequest owns its ordered IDs and normalized parameters.
// Catalog existence and route feasibility are service checks, not validation.
type ValidatedRecalculateRouteRequest struct {
	Request      ValidatedGenerateRouteRequest
	BasePlaceIDs []int64
	ChangeType   RouteChangeType
	PlaceID      int64
}

func ParseRecalculateRouteRequest(reader io.Reader) (ValidatedRecalculateRouteRequest, error) {
	var zero ValidatedRecalculateRouteRequest
	body, err := io.ReadAll(io.LimitReader(reader, MaxRouteRequestBytes+1))
	if err != nil {
		return zero, invalidRoute("Не удалось прочитать JSON")
	}
	if len(body) > MaxRouteRequestBytes {
		return zero, invalidRoute("Тело запроса превышает 64 КиБ")
	}
	if !utf8.Valid(body) {
		return zero, invalidRoute("JSON должен быть в UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := checkUniqueJSON(decoder); err != nil {
		return zero, invalidRoute("Некорректный JSON или повторные поля")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return zero, invalidRoute("Ожидается один JSON-документ")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return zero, invalidRoute("Ожидается JSON-объект")
	}
	if len(fields) != 3 || fields["request"] == nil || fields["base_place_ids"] == nil || fields["change"] == nil {
		return zero, invalidRoute("Обязательны только request, base_place_ids и change")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return zero, invalidRoute("Обязательные поля не могут быть null")
		}
	}
	request, err := ParseGenerateRouteRequest(bytes.NewReader(fields["request"]))
	if err != nil {
		return zero, err
	}
	var ids []int64
	if err := json.Unmarshal(fields["base_place_ids"], &ids); err != nil {
		return zero, invalidRoute("base_place_ids должен быть списком целых ID")
	}
	// Unmarshal accepts null as int64(0); reject it explicitly before value checks.
	var rawIDs []json.RawMessage
	if err := json.Unmarshal(fields["base_place_ids"], &rawIDs); err != nil {
		return zero, invalidRoute("Некорректный список ID")
	}
	for _, id := range rawIDs {
		if bytes.Equal(bytes.TrimSpace(id), []byte("null")) {
			return zero, invalidRoute("ID не может быть null")
		}
	}
	var changeFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["change"], &changeFields); err != nil || len(changeFields) != 2 || changeFields["type"] == nil || changeFields["place_id"] == nil {
		return zero, invalidRoute("change должен содержать только type и place_id")
	}
	for _, value := range changeFields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return zero, invalidRoute("Поля change не могут быть null")
		}
	}
	var change RouteChange
	if err := json.Unmarshal(fields["change"], &change); err != nil {
		return zero, invalidRoute("Некорректный тип поля change")
	}
	return validateRecalculateChange(request, ids, &change)
}

func ValidateRecalculateRouteRequest(input RecalculateRouteRequest) (ValidatedRecalculateRouteRequest, error) {
	if input.Request == nil {
		return ValidatedRecalculateRouteRequest{}, invalidRoute("Обязателен request")
	}
	request, err := ValidateGenerateRouteRequest(*input.Request)
	if err != nil {
		return ValidatedRecalculateRouteRequest{}, err
	}
	return validateRecalculateChange(request, input.BasePlaceIDs, input.Change)
}

func validateRecalculateChange(request ValidatedGenerateRouteRequest, ids []int64, change *RouteChange) (ValidatedRecalculateRouteRequest, error) {
	var zero ValidatedRecalculateRouteRequest
	if len(ids) == 0 {
		return zero, invalidRoute("Исходный маршрут не может быть пустым")
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return zero, invalidRoute("ID исходного маршрута должны быть положительными и уникальными")
		}
		seen[id] = true
	}
	if change == nil || change.PlaceID == nil || *change.PlaceID <= 0 {
		return zero, invalidRoute("Обязателен положительный place_id операции")
	}
	switch change.Type {
	case RouteChangeRemove:
		if !seen[*change.PlaceID] {
			return zero, invalidRoute("Удаляемое место отсутствует в исходном маршруте")
		}
	case RouteChangeAdd:
		if seen[*change.PlaceID] {
			return zero, invalidRoute("Добавляемое место уже есть в исходном маршруте")
		}
	default:
		return zero, invalidRoute("Допустимы только операции add и remove")
	}
	return ValidatedRecalculateRouteRequest{Request: request, BasePlaceIDs: append([]int64(nil), ids...), ChangeType: change.Type, PlaceID: *change.PlaceID}, nil
}

// ReadRecalculateRouteRequest writes the existing error envelope on failure.
func ReadRecalculateRouteRequest(w http.ResponseWriter, r *http.Request) (ValidatedRecalculateRouteRequest, bool) {
	requestlog.SetStage(r.Context(), "validation")
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		Error(w, 400, "invalid_request", "Ожидается Content-Type application/json")
		return ValidatedRecalculateRouteRequest{}, false
	}
	if r.Body == nil {
		r.Body = http.NoBody
	}
	defer r.Body.Close()
	result, err := ParseRecalculateRouteRequest(r.Body)
	if err != nil {
		if errors.Is(err, ErrInvalidRouteRequest) {
			Error(w, 400, "invalid_request", strings.TrimPrefix(err.Error(), ErrInvalidRouteRequest.Error()+": "))
		} else {
			failure(w, err)
		}
		return ValidatedRecalculateRouteRequest{}, false
	}
	return result, true
}
