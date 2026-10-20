package visits

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ Pool *pgxpool.Pool }

const visitColumns = `id,user_id,place_id,photo_id::text,idempotency_key::text,request_hash,status,rejection_code,created_at,confirmed_at,location_observed_at,distance_m,accuracy_m,deleted_at`

func scanVisit(row pgx.Row) (Visit, error) {
	var v Visit
	err := row.Scan(&v.ID, &v.UserID, &v.PlaceID, &v.PhotoID, &v.IdempotencyKey, &v.RequestHash, &v.Status, &v.RejectionCode, &v.CreatedAt, &v.ConfirmedAt, &v.LocationObservedAt, &v.DistanceM, &v.AccuracyM, &v.DeletedAt)
	return v, err
}
func (p Postgres) Confirm(ctx context.Context, c Command) (Result, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	// All confirmations for one owner serialize, including conflicting idempotency bodies.
	var owner int64
	if err = tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", c.Owner).Scan(&owner); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		return Result{}, err
	}
	existing, err := scanVisit(tx.QueryRow(ctx, "SELECT "+visitColumns+" FROM visits WHERE user_id=$1 AND idempotency_key=$2", c.Owner, c.Key))
	if err == nil {
		if !bytes.Equal(existing.RequestHash, c.Hash[:]) {
			return Result{}, ErrConflict
		}
		var xp int
		err = tx.QueryRow(ctx, "SELECT COALESCE((SELECT amount FROM xp_transactions WHERE visit_id=$1),0)", existing.ID).Scan(&xp)
		if err != nil {
			return Result{}, err
		}
		return Result{existing, xp, true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	var lat, lon float64
	var active bool
	err = tx.QueryRow(ctx, "SELECT lat,lon,is_active FROM places WHERE id=$1 FOR SHARE", c.Place).Scan(&lat, &lon, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	if !validCoordinates(lat, lon) {
		return Result{}, errors.New("invalid catalog coordinates")
	}
	var status PhotoStatus
	err = tx.QueryRow(ctx, `SELECT status FROM visit_photos WHERE id=$1 AND user_id=$2 AND place_id=$3 AND deleted_at IS NULL FOR UPDATE`, c.Input.PhotoID, c.Owner, c.Place).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	var used bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM visits WHERE photo_id=$1 AND status='confirmed')", c.Input.PhotoID).Scan(&used); err != nil {
		return Result{}, err
	}
	if used {
		return Result{}, ErrPhotoUsed
	}
	distance := DistanceM(lat, lon, c.Input.Location.Lat, c.Input.Location.Lon)
	code := c.Policy.reject(active, status == Ready, distance, c.Input.Location, c.ReceivedAt)
	storedTime := c.ReceivedAt.Truncate(time.Microsecond)
	v := Visit{UserID: c.Owner, PlaceID: c.Place, PhotoID: c.Input.PhotoID, IdempotencyKey: c.Key, RequestHash: c.Hash[:], Status: Confirmed, CreatedAt: storedTime, ConfirmedAt: &storedTime, LocationObservedAt: &c.Input.Location.ObservedAt, DistanceM: &distance, AccuracyM: &c.Input.Location.AccuracyM}
	if code != "" {
		v.Status = Rejected
		v.ConfirmedAt = nil
		v.RejectionCode = &code
	}
	v, err = scanVisit(tx.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,rejection_code,created_at,confirmed_at,location_observed_at,distance_m,accuracy_m)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+visitColumns, v.UserID, v.PlaceID, v.PhotoID, v.IdempotencyKey, v.RequestHash, v.Status, v.RejectionCode, v.CreatedAt, v.ConfirmedAt, v.LocationObservedAt, v.DistanceM, v.AccuracyM))
	if err != nil {
		return Result{}, err
	}
	xp := 0
	if v.Status == Confirmed {
		err = tx.QueryRow(ctx, `INSERT INTO xp_transactions(user_id,place_id,visit_id,amount,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,place_id) DO NOTHING RETURNING amount`, c.Owner, c.Place, v.ID, FirstVisitXP, c.ReceivedAt).Scan(&xp)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Result{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{v, xp, false}, nil
}
