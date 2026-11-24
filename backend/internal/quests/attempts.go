package quests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const attemptColumns = `a.id::text AS id,a.participation_id::text AS participation_id,s.id::text AS task_id,a.position,
	CASE WHEN ` + expiredPending + ` THEN 'expired' ELSE a.status END AS status,a.revision,
	COALESCE(f.status='ready' AND f.deleted_at IS NULL AND ` + photoInRetention + `,false) AS photo_ready,
	COALESCE(f.status='uploading' AND f.deleted_at IS NULL AND f.upload_expires_at>NOW(),false) AS uploading,
	a.submitted_at,a.reviewed_at,a.rejection_reason,a.xp_awarded`
const attemptFrom = ` FROM quest_attempts a
	JOIN quest_run_steps s ON s.run_id=a.run_id AND s.position=a.position
	LEFT JOIN visit_photos f ON f.id=a.photo_id `

func attemptFields(a *Attempt) []any {
	return []any{&a.ID, &a.ParticipationID, &a.TaskID, &a.Position, &a.Status, &a.Revision,
		&a.PhotoReady, &a.Uploading, &a.SubmittedAt, &a.ReviewedAt, &a.RejectionReason, &a.XPAwarded}
}

func scanAttempt(row scanner) (Attempt, error) {
	var a Attempt
	err := row.Scan(attemptFields(&a)...)
	return a, notFound(err)
}

func getAttempt(ctx context.Context, db queryRow, actor int64, id string) (Attempt, error) {
	return scanAttempt(db.QueryRow(ctx, "SELECT "+attemptColumns+attemptFrom+"WHERE a.id=$1 AND a.user_id=$2", id, actor))
}

func (p Postgres) GetAttempt(ctx context.Context, actor int64, id string) (Attempt, error) {
	return getAttempt(ctx, p.Pool, actor, id)
}

type participationLock struct {
	RunID    string
	Position *int
	Started  bool
	Finished bool
}

func lockParticipation(ctx context.Context, tx pgx.Tx, owner int64, id string) (participationLock, error) {
	var result participationLock
	err := tx.QueryRow(ctx, `SELECT run_id::text FROM quest_participations WHERE id=$1 AND user_id=$2`, id, owner).Scan(&result.RunID)
	if err != nil {
		return result, notFound(err)
	}
	// Fairly serialize new work against closing the run. Take the run lock
	// first, matching the order in which PostgreSQL cascades run deletion.
	err = tx.QueryRow(ctx, `SELECT started_at IS NOT NULL,finished_at IS NOT NULL
		FROM quest_runs WHERE id=$1 FOR SHARE`, result.RunID).Scan(&result.Started, &result.Finished)
	if err != nil {
		return result, notFound(err)
	}
	err = tx.QueryRow(ctx, `SELECT current_position FROM quest_participations WHERE id=$1 AND user_id=$2
		FOR UPDATE`, id, owner).Scan(&result.Position)
	return result, notFound(err)
}

func (p Postgres) CreateAttempt(ctx context.Context, owner int64, participationID, taskID string) (Attempt, error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Attempt, error) {
		part, err := lockParticipation(ctx, tx, owner, participationID)
		if err != nil {
			return Attempt{}, err
		}
		if !part.Started || part.Finished || part.Position == nil {
			return Attempt{}, ErrState
		}
		var place int64
		err = tx.QueryRow(ctx, `SELECT place_id FROM quest_run_steps WHERE run_id=$1 AND position=$2 AND id=$3`,
			part.RunID, *part.Position, taskID).Scan(&place)
		if err != nil {
			return Attempt{}, notFound(err)
		}
		if err = expireCurrentAttempt(ctx, tx, participationID, *part.Position); err != nil {
			return Attempt{}, err
		}
		var id string
		err = tx.QueryRow(ctx, `SELECT id::text FROM quest_attempts WHERE participation_id=$1 AND position=$2
			AND status IN ('draft','pending_review')`, participationID, *part.Position).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO quest_attempts(participation_id,run_id,user_id,position,place_id)
				VALUES($1,$2,$3,$4,$5) RETURNING id::text`, participationID, part.RunID, owner, *part.Position, place).Scan(&id)
		}
		if err != nil {
			return Attempt{}, err
		}
		return getAttempt(ctx, tx, owner, id)
	})
}

type lockedAttempt struct {
	ParticipationID string
	Part            participationLock
	Position        int
	PlaceID         int64
	Status          string
	Revision        int64
	PhotoID         *string
	UploadHash      []byte

	SubmissionHash []byte
	ObservedAt     *time.Time
	Distance       *float64
	Accuracy       *float64
	Reason         *string
	PhotoExpired   bool
}

func (a lockedAttempt) isCurrent() bool {
	return a.Part.Started && a.Part.Position != nil && *a.Part.Position == a.Position
}

func lockAttempt(ctx context.Context, tx pgx.Tx, owner int64, id string) (lockedAttempt, error) {
	var a lockedAttempt
	err := tx.QueryRow(ctx, `SELECT participation_id::text FROM quest_attempts WHERE id=$1 AND user_id=$2`, id, owner).Scan(&a.ParticipationID)
	if err != nil {
		return a, notFound(err)
	}
	a.Part, err = lockParticipation(ctx, tx, owner, a.ParticipationID)
	if err != nil {
		return a, err
	}
	err = tx.QueryRow(ctx, `SELECT position,place_id,status,revision,photo_id::text,upload_hash,
		submission_hash,location_observed_at,distance_m,accuracy_m,rejection_reason,
		COALESCE(photo_expires_at<=clock_timestamp(),false)
		FROM quest_attempts WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, owner).
		Scan(&a.Position, &a.PlaceID, &a.Status, &a.Revision, &a.PhotoID, &a.UploadHash,
			&a.SubmissionHash, &a.ObservedAt, &a.Distance, &a.Accuracy, &a.Reason, &a.PhotoExpired)
	return a, notFound(err)
}

func lockAccount(ctx context.Context, tx pgx.Tx, user int64) error {
	var id int64
	return notFound(tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", user).Scan(&id))
}

func requireReadyPhoto(ctx context.Context, tx pgx.Tx, id *string) error {
	if id == nil {
		return ErrPhotoNotReady
	}
	var photo string
	err := tx.QueryRow(ctx, `SELECT id::text FROM visit_photos WHERE id=$1
		AND purpose='quest' AND status='ready' AND deleted_at IS NULL FOR UPDATE`, *id).Scan(&photo)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPhotoNotReady
	}
	return err
}

func (p Postgres) Submit(ctx context.Context, owner int64, id, key string, loc visits.Location, received time.Time) (Attempt, error) {
	if !visits.ValidLocation(loc) {
		return Attempt{}, &Invalid{"location", "Укажите координаты, точность и время измерения GPS"}
	}
	loc.ObservedAt = loc.ObservedAt.UTC()
	if loc.Lat == 0 {
		loc.Lat = 0
	}
	if loc.Lon == 0 {
		loc.Lon = 0
	}
	if loc.AccuracyM == 0 {
		loc.AccuracyM = 0
	}
	encoded, err := json.Marshal(loc)
	if err != nil {
		return Attempt{}, &Invalid{"location", "Некорректная геопозиция"}
	}
	hash := sha256.Sum256(encoded)
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Attempt, error) {
		a, err := lockAttempt(ctx, tx, owner, id)
		if err != nil {
			return Attempt{}, err
		}
		var previous string
		err = tx.QueryRow(ctx, `SELECT id::text FROM quest_attempts WHERE participation_id=$1 AND submission_key=$2`,
			a.ParticipationID, key).Scan(&previous)
		switch {
		case err == nil:
			if previous != id || !bytes.Equal(a.SubmissionHash, hash[:]) {
				return Attempt{}, ErrSubmissionConflict
			}
			return getAttempt(ctx, tx, owner, id)
		case !errors.Is(err, pgx.ErrNoRows):
			return Attempt{}, err
		}
		if a.Status != draft || !a.isCurrent() || a.Part.Finished {
			return Attempt{}, ErrState
		}
		if err = requireReadyPhoto(ctx, tx, a.PhotoID); err != nil {
			return Attempt{}, err
		}
		var policy visits.Policy
		var lat, lon float64
		var age, skew int
		err = tx.QueryRow(ctx, `SELECT s.lat,s.lon,r.radius_m,r.accuracy_m,r.location_max_age_seconds,r.location_future_skew_seconds
			FROM quest_runs r JOIN quest_run_steps s ON s.run_id=r.id WHERE r.id=$1 AND s.position=$2`, a.Part.RunID, a.Position).
			Scan(&lat, &lon, &policy.RadiusM, &policy.AccuracyM, &age, &skew)
		if err != nil {
			return Attempt{}, err
		}
		policy.MaxAge = time.Duration(age) * time.Second
		policy.FutureSkew = time.Duration(skew) * time.Second
		distance, code := policy.CheckLocation(lat, lon, loc, received)
		if code != "" {
			return Attempt{}, &LocationRejection{code}
		}
		_, err = tx.Exec(ctx, `UPDATE quest_attempts SET status='pending_review', revision=revision+1, submission_key=$2,
			submission_hash=$3,submitted_at=$4,location_observed_at=$5,distance_m=$6,accuracy_m=$7,
			photo_expires_at=clock_timestamp()+INTERVAL '168 hours' WHERE id=$1`,
			id, key, hash[:], received, loc.ObservedAt, distance, loc.AccuracyM)
		if err != nil {
			return Attempt{}, err
		}
		return getAttempt(ctx, tx, owner, id)
	})
}

func (p Postgres) Withdraw(ctx context.Context, owner int64, id string) (Attempt, error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Attempt, error) {
		a, err := lockAttempt(ctx, tx, owner, id)
		if err != nil {
			return Attempt{}, err
		}
		if a.Status == withdrawn {
			return getAttempt(ctx, tx, owner, id)
		}
		if a.Status != draft && a.Status != pendingReview {
			return Attempt{}, ErrState
		}
		if a.PhotoID != nil {
			if _, err = tx.Exec(ctx, `UPDATE visit_photos SET deleted_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, *a.PhotoID); err != nil {
				return Attempt{}, err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE quest_attempts SET status='withdrawn',revision=revision+1,photo_expires_at=NULL WHERE id=$1`, id); err != nil {
			return Attempt{}, err
		}
		return getAttempt(ctx, tx, owner, id)
	})
}
