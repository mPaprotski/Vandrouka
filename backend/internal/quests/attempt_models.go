package quests

import (
	"errors"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

var (
	ErrUploadInProgress   = errors.New("quest photo upload is in progress")
	ErrUploadConflict     = errors.New("quest photo already uploaded with a different body")
	ErrSubmissionConflict = errors.New("quest submission key already used")
	ErrPhotoExpired       = errors.New("quest photo review expired")
	ErrPhotoNotReady      = errors.New("quest photo is not available")
)

type LocationRejection struct{ Code visits.RejectionCode }

func (e *LocationRejection) Error() string { return string(e.Code) }

const (
	draft         = "draft"
	pendingReview = "pending_review"
	accepted      = "accepted"
	rejected      = "rejected"
	withdrawn     = "withdrawn"
)

type Attempt struct {
	ID              string     `json:"id"`
	ParticipationID string     `json:"participation_id"`
	TaskID          string     `json:"task_id"`
	Position        int        `json:"position"`
	Status          string     `json:"status"`
	Revision        int64      `json:"revision"`
	PhotoReady      bool       `json:"photo_ready"`
	Uploading       bool       `json:"uploading"`
	SubmittedAt     *time.Time `json:"submitted_at,omitempty"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	RejectionReason *string    `json:"rejection_reason,omitempty"`
	XPAwarded       int        `json:"xp_awarded"`
}

type Story struct {
	TaskID    string        `json:"task_id"`
	Position  int           `json:"position"`
	PlaceID   int64         `json:"place_id"`
	PlaceName string        `json:"place_name"`
	Address   string        `json:"address"`
	Text      string        `json:"text"`
	Links     []places.Link `json:"links"`
	AttemptID string        `json:"attempt_id"`
	XPAwarded int           `json:"xp_awarded"`
}

type ReviewItem struct {
	Attempt
	ParticipantName string  `json:"participant_name"`
	PlaceName       string  `json:"place_name"`
	PhotoTask       string  `json:"photo_task"`
	DistanceM       float64 `json:"distance_m"`
	AccuracyM       float64 `json:"accuracy_m"`
}

type ReviewInput struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
}
