package routing

import "context"

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}
type Leg struct {
	Seconds    float64 `json:"seconds"`
	Kilometers float64 `json:"kilometers"`
	Reachable  bool    `json:"reachable"`
}
type Matrix [][]Leg

type Router interface {
	Matrix(context.Context, []Point) (Matrix, error)
}

type Route struct {
	Legs       []Leg    `json:"legs,omitempty"`
	Shapes     []string `json:"shapes"`
	Seconds    float64  `json:"seconds"`
	Kilometers float64  `json:"kilometers"`
}
