package quests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yourname/vandrouka/backend/internal/media"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

type uploadReservation struct {
	PhotoID string
	Replay  bool
}

func (p Postgres) reservePhoto(ctx context.Context, owner int64, attemptID, mime string, hash []byte) (uploadReservation, error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (uploadReservation, error) {
		if err := lockAccount(ctx, tx, owner); err != nil {
			return uploadReservation{}, err
		}
		a, err := lockAttempt(ctx, tx, owner, attemptID)
		if err != nil {
			return uploadReservation{}, err
		}
		if a.PhotoExpired || a.Status == "expired" {
			return uploadReservation{}, ErrPhotoExpired
		}
		if a.PhotoID != nil {
			var status string
			var deleted, expired bool
			err = tx.QueryRow(ctx, `SELECT status,deleted_at IS NOT NULL,upload_expires_at<=NOW()
				FROM visit_photos WHERE id=$1 AND purpose='quest' FOR UPDATE NOWAIT`, *a.PhotoID).Scan(&status, &deleted, &expired)
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
				return uploadReservation{}, ErrUploadInProgress
			}
			if err != nil {
				return uploadReservation{}, err
			}
			if !deleted && status == "ready" {
				if !bytes.Equal(a.UploadHash, hash) {
					return uploadReservation{}, ErrUploadConflict
				}
				return uploadReservation{*a.PhotoID, true}, nil
			}
			if !deleted && status == "uploading" && !expired {
				return uploadReservation{}, ErrUploadInProgress
			}
		}
		if a.Status != draft || !a.isCurrent() || a.Part.Finished {
			return uploadReservation{}, ErrState
		}
		if a.PhotoID != nil {
			_, err = tx.Exec(ctx, `UPDATE visit_photos SET deleted_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, *a.PhotoID)
			if err != nil {
				return uploadReservation{}, err
			}
		}
		ext := ".jpg"
		if mime == "image/png" {
			ext = ".png"
		}
		var id string
		err = tx.QueryRow(ctx, `WITH photo AS (SELECT gen_random_uuid() AS id)
			INSERT INTO visit_photos(id,user_id,place_id,object_key,upload_expires_at,purpose)
			SELECT id,$1,$2,'quest-photos/'||id::text||$3,NOW()+INTERVAL '2 minutes','quest' FROM photo RETURNING id::text`,
			owner, a.PlaceID, ext).Scan(&id)
		if err != nil {
			return uploadReservation{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE quest_attempts SET photo_id=$2,upload_hash=$3,revision=revision+1 WHERE id=$1`, attemptID, id, hash)
		return uploadReservation{PhotoID: id}, err
	})
}

func (p Postgres) UploadPhoto(ctx context.Context, owner int64, id string, raw []byte, mime string, photos *media.Service) (Attempt, error) {
	if photos.Anonymizer == nil {
		return Attempt{}, media.ErrAnonymizerUnavailable
	}
	digest := sha256.New()
	digest.Write([]byte(mime))
	digest.Write([]byte{0})
	digest.Write(raw)
	reserved, err := p.reservePhoto(ctx, owner, id, mime, digest.Sum(nil))
	if err != nil {
		return Attempt{}, err
	}
	if !reserved.Replay {
		prepared, err := photos.PrepareQuest(ctx, bytes.NewReader(raw), mime)
		if err == nil {
			err = photos.SaveQuest(ctx, owner, reserved.PhotoID, prepared)
		}
		if err != nil {
			// A commit can succeed after the HTTP client loses its response. Only
			// abandon unfinished uploads and leave ready reservations replayable.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_, _ = p.Pool.Exec(cleanup, `UPDATE visit_photos SET status='failed',deleted_at=NOW()
				WHERE id=$1 AND status='uploading' AND deleted_at IS NULL`, reserved.PhotoID)
			return Attempt{}, err
		}
	}
	return getAttempt(ctx, p.Pool, owner, id)
}

func (p Postgres) PhotoForActor(ctx context.Context, actor int64, attemptID string) (visits.Photo, error) {
	var photo visits.Photo
	err := p.Pool.QueryRow(ctx, `SELECT f.id::text,f.object_key,f.mime_type,f.size_bytes,f.sha256
		FROM quest_attempts a JOIN quest_runs r ON r.id=a.run_id JOIN quests q ON q.id=r.quest_id
		JOIN visit_photos f ON f.id=a.photo_id WHERE a.id=$1 AND a.status NOT IN ('withdrawn','expired') AND `+photoInRetention+`
		AND (a.user_id=$2 OR (q.owner_id=$2 AND a.status IN ('pending_review','accepted','rejected')))
		AND f.purpose='quest' AND f.status='ready' AND f.deleted_at IS NULL`, attemptID, actor).
		Scan(&photo.ID, &photo.ObjectKey, &photo.MIMEType, &photo.SizeBytes, &photo.SHA256)
	return photo, notFound(err)
}
