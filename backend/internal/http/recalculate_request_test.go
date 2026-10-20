package apihttp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const validRecalculateBody = `{"request":{"start":{"lat":53.9,"lon":27.5},"start_at":"2026-10-24t07:00:00z","budget_minutes":120},"base_place_ids":[3,1,2],"change":{"type":"remove","place_id":1}}`

func TestParseRecalculateDefaultsAndBoundaryValues(t *testing.T) {
	result, err := ParseRecalculateRouteRequest(strings.NewReader(validRecalculateBody))
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.City != "minsk" || result.Request.Pace != "normal" || result.Request.Seed != 42 || !result.Request.IncludeGeometry || result.Request.TicketBudgetMinor != nil || result.Request.StartAt.Hour() != 10 || result.Request.StartAt.Location().String() != "Europe/Minsk" || !reflect.DeepEqual(result.BasePlaceIDs, []int64{3, 1, 2}) || result.ChangeType != RouteChangeRemove || result.PlaceID != 1 {
		t.Fatalf("unexpected %+v", result)
	}
	body := strings.Replace(validRecalculateBody, `"budget_minutes":120`, `"budget_minutes":1,"ticket_budget_minor":0,"seed":0,"include_geometry":false`, 1)
	body = strings.Replace(body, `"type":"remove","place_id":1`, `"type":"add","place_id":9223372036854775807`, 1)
	result, err = ParseRecalculateRouteRequest(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if result.PlaceID != 9223372036854775807 || result.Request.TicketBudgetMinor == nil || *result.Request.TicketBudgetMinor != 0 || result.Request.IncludeGeometry || result.Request.Seed != 0 {
		t.Fatal("explicit values lost")
	}
	// Limit applies to the complete wrapper; exact-size JSON including whitespace is valid.
	exact := validRecalculateBody + strings.Repeat(" ", MaxRouteRequestBytes-len(validRecalculateBody))
	if _, err := ParseRecalculateRouteRequest(strings.NewReader(exact)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRecalculateRouteRequest(strings.NewReader(exact + " ")); !errors.Is(err, ErrInvalidRouteRequest) {
		t.Fatal("oversize accepted")
	}
}

func TestParseRecalculateRejectsAmbiguousAndInvalidRequests(t *testing.T) {
	cases := map[string]string{"empty": "", "null": "null", "array": "[]", "trailing": validRecalculateBody + " {}", "utf8": validRecalculateBody + string([]byte{255})}
	for name, replacement := range map[string]string{
		"empty IDs": `[]`, "null IDs": `null`, "duplicate IDs": `[1,1]`, "negative ID": `[-1]`, "zero ID": `[0]`, "null item": `[null]`, "fractional ID": `[1.5]`, "string ID": `["1"]`, "overflow ID": `[9223372036854775808]`, "object IDs": `{}`} {
		cases[name] = strings.Replace(validRecalculateBody, `[3,1,2]`, replacement, 1)
	}
	for name, replacement := range map[string]string{
		"missing change": `{}`, "null change": `null`, "pin": `{"type":"pin","place_id":1}`, "missing type": `{"place_id":1}`, "null type": `{"type":null,"place_id":1}`, "unknown field": `{"type":"remove","place_id":1,"x":1}`, "uppercase field": `{"Type":"remove","place_id":1}`, "missing place": `{"type":"remove"}`, "null place": `{"type":"remove","place_id":null}`, "zero place": `{"type":"remove","place_id":0}`, "fractional place": `{"type":"remove","place_id":1.1}`, "absent remove": `{"type":"remove","place_id":4}`, "existing add": `{"type":"add","place_id":1}`, "duplicate change key": `{"type":"remove","type":"add","place_id":4}`,
	} {
		cases[name] = strings.Replace(validRecalculateBody, `{"type":"remove","place_id":1}`, replacement, 1)
	}
	cases["duplicate outer key"] = strings.Replace(validRecalculateBody, `"base_place_ids":[3,1,2]`, `"base_place_ids":[3,1,2],"base_place_ids":[3,1,2]`, 1)
	cases["duplicate nested key"] = strings.Replace(validRecalculateBody, `"budget_minutes":120`, `"budget_minutes":120,"budget_minutes":120`, 1)
	cases["unknown nested key"] = strings.Replace(validRecalculateBody, `"budget_minutes":120`, `"budget_minutes":120,"extra":true`, 1)
	cases["invalid nested budget"] = strings.Replace(validRecalculateBody, `"budget_minutes":120`, `"budget_minutes":0`, 1)
	cases["unknown wrapper field"] = strings.Replace(validRecalculateBody, `"request":`, `"extra":1,"request":`, 1)
	cases["null request"] = `{"request":null,"base_place_ids":[1],"change":{"type":"remove","place_id":1}}`
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRecalculateRouteRequest(strings.NewReader(body)); !errors.Is(err, ErrInvalidRouteRequest) {
				t.Fatalf("accepted invalid request or wrong error: %v", err)
			}
		})
	}
}

func TestValidateRecalculateOwnsInputAndChecksOperation(t *testing.T) {
	// Uppercase timestamps allow the transport model's standard time decoder.
	var input RecalculateRouteRequest
	body := strings.ReplaceAll(strings.ReplaceAll(validRecalculateBody, "24t", "24T"), "00z", "00Z")
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatal(err)
	}
	result, err := ValidateRecalculateRouteRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.BasePlaceIDs[0] = 99
	*input.Change.PlaceID = 2
	if result.BasePlaceIDs[0] != 3 || result.PlaceID != 1 {
		t.Fatal("validated request aliases input")
	}
	input.Request = nil
	if _, err := ValidateRecalculateRouteRequest(input); !errors.Is(err, ErrInvalidRouteRequest) {
		t.Fatal("missing request accepted")
	}
}

type brokenRecalculateReader struct{}

func (brokenRecalculateReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestReadRecalculateHTTPBoundary(t *testing.T) {
	if _, err := ParseRecalculateRouteRequest(brokenRecalculateReader{}); !errors.Is(err, ErrInvalidRouteRequest) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		content, body string
		valid         bool
	}{
		{"application/json; charset=utf-8", validRecalculateBody, true}, {"text/plain", validRecalculateBody, false}, {"application/json", `{}`, false}, {"", validRecalculateBody, false},
	} {
		req := httptest.NewRequest("POST", "/v1/routes/recalculate", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.content)
		w := httptest.NewRecorder()
		_, ok := ReadRecalculateRouteRequest(w, req)
		if ok != tc.valid {
			t.Fatal("unexpected validation result")
		}
		if ok {
			if w.Body.Len() != 0 {
				t.Fatal("success wrote response")
			}
		} else {
			var e APIError
			if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != "invalid_request" {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		}
	}
}
