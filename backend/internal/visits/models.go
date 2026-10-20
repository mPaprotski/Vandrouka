// Package visits defines persistence contracts for the private exploration loop.
// Upload and confirmation services are implemented in the following stages.
package visits

import "time"

const FirstVisitXP = 100

type Status string

const (
	Confirmed Status = "confirmed"
	Rejected  Status = "rejected"
)

type PhotoStatus string

const (
	Uploading PhotoStatus = "uploading"
	Ready     PhotoStatus = "ready"
	Failed    PhotoStatus = "failed"
)

type RejectionCode string

const (
	TooFar         RejectionCode = "too_far"
	PoorAccuracy   RejectionCode = "poor_accuracy"
	StaleLocation  RejectionCode = "stale_location"
	FutureLocation RejectionCode = "future_location"
	InactivePlace  RejectionCode = "inactive_place"
	PhotoNotReady  RejectionCode = "photo_not_ready"
)

// Photo has no public storage URL; ObjectKey is internal and must never be serialized.
type Photo struct {
	ID              string      `json:"id"`
	UserID          int64       `json:"-"`
	PlaceID         int64       `json:"place_id"`
	ObjectKey       string      `json:"-"`
	Status          PhotoStatus `json:"status"`
	MIMEType        *string     `json:"mime_type,omitempty"`
	SizeBytes       *int64      `json:"size_bytes,omitempty"`
	Width           *int        `json:"width,omitempty"`
	Height          *int        `json:"height,omitempty"`
	SHA256          []byte      `json:"-"`
	CreatedAt       time.Time   `json:"created_at"`
	UploadExpiresAt time.Time   `json:"-"`
	ReadyAt         *time.Time  `json:"ready_at,omitempty"`
	DeletedAt       *time.Time  `json:"-"`
}
type Visit struct {
	ID                 int64          `json:"id"`
	UserID             int64          `json:"-"`
	PlaceID            int64          `json:"place_id"`
	PhotoID            string         `json:"photo_id"`
	IdempotencyKey     string         `json:"-"`
	RequestHash        []byte         `json:"-"`
	Status             Status         `json:"status"`
	RejectionCode      *RejectionCode `json:"rejection_code,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	ConfirmedAt        *time.Time     `json:"confirmed_at,omitempty"`
	LocationObservedAt *time.Time     `json:"-"`
	DistanceM          *float64       `json:"-"`
	AccuracyM          *float64       `json:"-"`
	DeletedAt          *time.Time     `json:"-"`
}
type XPTransaction struct {
	ID        int64
	UserID    int64
	PlaceID   int64
	VisitID   int64
	Amount    int
	CreatedAt time.Time
}
