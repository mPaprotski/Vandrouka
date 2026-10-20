package visits

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalid   = errors.New("invalid visit request")
	ErrNotFound  = errors.New("visit resource not found")
	ErrConflict  = errors.New("idempotency conflict")
	ErrPhotoUsed = errors.New("photo already confirmed")
)

type Location struct {
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	AccuracyM  float64   `json:"accuracy_m"`
	ObservedAt time.Time `json:"observed_at"`
}
type Input struct {
	PhotoID  string   `json:"photo_id"`
	Location Location `json:"location"`
}
type Result struct {
	Visit     Visit `json:"visit"`
	XPAwarded int   `json:"xp_awarded"`
	Replay    bool  `json:"-"`
}
type Policy struct {
	RadiusM    float64
	AccuracyM  float64
	MaxAge     time.Duration
	FutureSkew time.Duration
}

func DefaultPolicy() Policy { return Policy{50, 30, 120 * time.Second, 30 * time.Second} }
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validCoordinates(lat, lon float64) bool {
	return finite(lat) && finite(lon) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
func (p Policy) Validate() error {
	if !finite(p.RadiusM) || p.RadiusM <= 0 || !finite(p.AccuracyM) || p.AccuracyM <= 0 || p.MaxAge <= 0 || p.FutureSkew < 0 {
		return ErrInvalid
	}
	return nil
}

// DistanceM uses the mean Earth radius (IUGG), never a routing provider or rounded distance.
func DistanceM(lat1, lon1, lat2, lon2 float64) float64 {
	const rad = math.Pi / 180
	a := math.Pow(math.Sin((lat2-lat1)*rad/2), 2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Pow(math.Sin((lon2-lon1)*rad/2), 2)
	return 6371008.8 * 2 * math.Asin(math.Sqrt(math.Max(0, math.Min(1, a))))
}

// Rejection precedence is stable when more than one business rule fails.
func (p Policy) reject(active, ready bool, distance float64, l Location, received time.Time) RejectionCode {
	switch {
	case !active:
		return InactivePlace
	case !ready:
		return PhotoNotReady
	case l.AccuracyM > p.AccuracyM:
		return PoorAccuracy
	case l.ObservedAt.Before(received.Add(-p.MaxAge)):
		return StaleLocation
	case l.ObservedAt.After(received.Add(p.FutureSkew)):
		return FutureLocation
	case distance > p.RadiusM:
		return TooFar
	default:
		return ""
	}
}

type Command struct {
	Owner, Place int64
	Key          string
	Input        Input
	ReceivedAt   time.Time
	Hash         [32]byte
	Policy       Policy
}
type Repository interface {
	Confirm(context.Context, Command) (Result, error)
}
type Service struct {
	repo   Repository
	policy Policy
}

func New(repo Repository, policy Policy) (*Service, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Service{repo, policy}, nil
}
func (s *Service) Confirm(ctx context.Context, owner, place int64, key string, in Input, received time.Time) (Result, error) {
	// UUID syntax is canonical apart from case; alternative encodings are not accepted.
	photo, err := uuid.Parse(in.PhotoID)
	if err != nil || len(in.PhotoID) != 36 || !strings.EqualFold(photo.String(), in.PhotoID) {
		return Result{}, ErrInvalid
	}
	id, err := uuid.Parse(key)
	if err != nil || len(key) != 36 || !strings.EqualFold(id.String(), key) {
		return Result{}, ErrInvalid
	}
	l := in.Location
	if owner < 1 || place < 1 || !validCoordinates(l.Lat, l.Lon) || !finite(l.AccuracyM) || l.AccuracyM < 0 || l.ObservedAt.IsZero() || received.IsZero() {
		return Result{}, ErrInvalid
	}
	in.PhotoID = photo.String()
	in.Location.ObservedAt = l.ObservedAt.UTC()
	// Normalize negative zero and equivalent offsets/numeric JSON representations.
	if in.Location.Lat == 0 {
		in.Location.Lat = 0
	}
	if in.Location.Lon == 0 {
		in.Location.Lon = 0
	}
	if in.Location.AccuracyM == 0 {
		in.Location.AccuracyM = 0
	}
	body, err := json.Marshal(struct {
		Place int64
		Input Input
	}{place, in})
	if err != nil {
		return Result{}, ErrInvalid
	}
	return s.repo.Confirm(ctx, Command{owner, place, id.String(), in, received.UTC(), sha256.Sum256(body), s.policy})
}
