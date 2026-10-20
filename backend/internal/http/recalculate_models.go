package apihttp

import "encoding/json"

// RecalculateRouteRequest accepts the original stop order, not client totals.
// Required objects retain presence for validation in the next stage.
type RecalculateRouteRequest struct {
	Request      *GenerateRouteRequest `json:"request"`
	BasePlaceIDs []int64               `json:"base_place_ids"`
	Change       *RouteChange          `json:"change"`
}

type RouteChangeType string

const (
	RouteChangeRemove RouteChangeType = "remove"
	RouteChangeAdd    RouteChangeType = "add"
)

// RouteChange describes one operation. Pin is outside the current contract.
type RouteChange struct {
	Type    RouteChangeType `json:"type"`
	PlaceID *int64          `json:"place_id"`
}

type RecalculateRouteResponse struct {
	Seed     int64        `json:"seed"`
	Timezone string       `json:"timezone"`
	Variant  RouteVariant `json:"variant"`
	Diff     RouteDiff    `json:"diff"`
}

// Deltas are signed result-minus-baseline values. Cost uses integer BYN kopecks.
// Added IDs follow the new order; removed IDs follow the original order.
// PricesConfirmed requires known prices in both routes.
type RouteDiff struct {
	TotalSecondsDelta float64 `json:"total_seconds_delta"`
	DistanceMDelta    float64 `json:"distance_m_delta"`
	CostMinorDelta    int64   `json:"cost_minor_delta"`
	AddedPlaceIDs     []int64 `json:"added_place_ids"`
	RemovedPlaceIDs   []int64 `json:"removed_place_ids"`
	PricesConfirmed   bool    `json:"prices_confirmed"`
	Text              string  `json:"text"`
}

func (r RouteDiff) MarshalJSON() ([]byte, error) {
	type alias RouteDiff
	r.AddedPlaceIDs = nonNilSlice(r.AddedPlaceIDs)
	r.RemovedPlaceIDs = nonNilSlice(r.RemovedPlaceIDs)
	return json.Marshal(alias(r))
}
