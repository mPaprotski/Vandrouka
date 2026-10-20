package media

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

type Postgres struct{ Pool *pgxpool.Pool }

const photoColumns = `id::text,user_id,place_id,object_key,status,mime_type,size_bytes,width,height,sha256,created_at,upload_expires_at,ready_at,deleted_at`

func scanPhoto(row pgx.Row) (visits.Photo, error) {
	var p visits.Photo
	err := row.Scan(&p.ID, &p.UserID, &p.PlaceID, &p.ObjectKey, &p.Status, &p.MIMEType, &p.SizeBytes, &p.Width, &p.Height, &p.SHA256, &p.CreatedAt, &p.UploadExpiresAt, &p.ReadyAt, &p.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, ErrUnavailable
	}
	return p, nil
}
func (p Postgres) Create(ctx context.Context, owner, place int64, mime string) (visits.Photo, error) {
	extension := ".jpg"
	if mime == "image/png" {
		extension = ".png"
	}
	return scanPhoto(p.Pool.QueryRow(ctx, `WITH new_id AS (SELECT gen_random_uuid() AS id)
 INSERT INTO visit_photos(id,user_id,place_id,object_key,upload_expires_at)
 SELECT new_id.id,$1,p.id,'visit-photos/'||new_id.id::text||$3,NOW()+INTERVAL '15 minutes'
 FROM places p CROSS JOIN new_id WHERE p.id=$2 AND p.is_active RETURNING `+photoColumns, owner, place, extension))
}
func (p Postgres) Complete(ctx context.Context, owner int64, id string, asset Image, put func(context.Context, string) error) (visits.Photo, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return visits.Photo{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	photo, err := scanPhoto(tx.QueryRow(ctx, "SELECT "+photoColumns+" FROM visit_photos WHERE id=$1 AND user_id=$2 AND status='uploading' AND deleted_at IS NULL AND upload_expires_at>NOW() FOR UPDATE", id, owner))
	if err != nil {
		return visits.Photo{}, err
	}
	if err = put(ctx, photo.ObjectKey); err != nil {
		return visits.Photo{}, ErrUnavailable
	}
	ready, err := scanPhoto(tx.QueryRow(ctx, `UPDATE visit_photos SET status='ready',mime_type=$3,size_bytes=$4,width=$5,height=$6,sha256=$7,ready_at=NOW() WHERE id=$1 AND user_id=$2 RETURNING `+photoColumns, id, owner, asset.MIME, len(asset.Body), asset.Width, asset.Height, asset.SHA256))
	if err != nil {
		return visits.Photo{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return visits.Photo{}, ErrUnavailable
	}
	return ready, nil
}
func (p Postgres) Abort(ctx context.Context, owner int64, id string) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `WITH abandoned AS (
 UPDATE visit_photos SET deleted_at=NOW(),status=CASE WHEN status='uploading' THEN 'failed' ELSE status END
 WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL RETURNING object_key,upload_expires_at)
 SELECT 1 FROM abandoned`, id, owner)
	if err != nil {
		return ErrUnavailable
	}
	// The AFTER trigger enqueues the key. Delay abandoned writes until their lease expires.
	_, err = tx.Exec(ctx, `UPDATE media_deletions d SET not_before=GREATEST(d.not_before,p.upload_expires_at)
 FROM visit_photos p WHERE p.id=$1 AND p.user_id=$2 AND d.object_key=p.object_key`, id, owner)
	if err != nil {
		return ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}
func (p Postgres) Read(ctx context.Context, owner int64, id string) (visits.Photo, error) {
	return scanPhoto(p.Pool.QueryRow(ctx, "SELECT "+photoColumns+" FROM visit_photos WHERE id=$1 AND user_id=$2 AND status='ready' AND deleted_at IS NULL", id, owner))
}
func (p Postgres) Tombstone(ctx context.Context, owner int64, id string, unconfirmedOnly bool) (string, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return "", ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var key string
	err = tx.QueryRow(ctx, "SELECT object_key FROM visit_photos WHERE id=$1 AND user_id=$2 FOR UPDATE", id, owner).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrUnavailable
	}
	if unconfirmedOnly {
		// A new statement after acquiring the photo lock sees a confirmation that
		// committed while this transaction was waiting for the lock.
		var confirmed bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM visits WHERE photo_id=$1 AND status='confirmed')", id).Scan(&confirmed); err != nil {
			return "", ErrUnavailable
		}
		if confirmed {
			return "", ErrConfirmed
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE visit_photos SET deleted_at=COALESCE(deleted_at,NOW()) WHERE id=$1", id); err != nil {
		return "", ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return "", ErrUnavailable
	}
	return key, nil
}
func (p Postgres) Purge(ctx context.Context, key string, remove func(context.Context, string) error) error {
	if err := remove(ctx, key); err != nil {
		return ErrUnavailable
	}
	if _, err := p.Pool.Exec(ctx, "DELETE FROM media_deletions WHERE object_key=$1", key); err != nil {
		return ErrUnavailable
	}
	return nil
}
func (p Postgres) Cleanup(ctx context.Context, now, unusedBefore time.Time, remove func(context.Context, string) error) error {

	// Lock candidates first, then use a fresh statement snapshot for the confirmed
	// visit check. A single CTE snapshot can miss a concurrently committed visit
	// even if its row lock is acquired after that commit.
	selection, err := p.Pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer selection.Rollback(ctx)
	rows, err := selection.Query(ctx, `SELECT p.id::text FROM visit_photos p WHERE p.deleted_at IS NULL AND
 ((p.status='uploading' AND p.upload_expires_at<=$1) OR p.status='failed' OR
 (p.status='ready' AND p.created_at<$2 AND NOT EXISTS(SELECT 1 FROM visits v WHERE v.photo_id=p.id AND v.status='confirmed')))
 ORDER BY p.created_at,p.id LIMIT 25 FOR UPDATE OF p SKIP LOCKED`, now, unusedBefore)
	if err != nil {
		return ErrUnavailable
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return ErrUnavailable
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return ErrUnavailable
	}
	if len(ids) > 0 {
		_, err = selection.Exec(ctx, `UPDATE visit_photos p SET deleted_at=$1,status=CASE WHEN p.status='uploading' THEN 'failed' ELSE p.status END
   WHERE p.id=ANY($3::uuid[]) AND p.deleted_at IS NULL
   AND ((p.status='uploading' AND p.upload_expires_at<=$1) OR p.status='failed' OR (p.status='ready' AND p.created_at<$2))
   AND NOT EXISTS(SELECT 1 FROM visits v WHERE v.photo_id=p.id AND v.status='confirmed')`, now, unusedBefore, ids)
		if err != nil {
			return ErrUnavailable
		}
	}
	if err = selection.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	failed := false
	for i := 0; i < 25; i++ {
		tx, err := p.Pool.Begin(ctx)
		if err != nil {
			return ErrUnavailable
		}
		var key string
		err = tx.QueryRow(ctx, "SELECT object_key FROM media_deletions WHERE not_before<=$1 ORDER BY not_before,object_key LIMIT 1 FOR UPDATE SKIP LOCKED", now).Scan(&key)
		if errors.Is(err, pgx.ErrNoRows) {
			tx.Rollback(ctx)
			break
		}
		if err != nil {
			tx.Rollback(ctx)
			return ErrUnavailable
		}
		if err = remove(ctx, key); err != nil {
			failed = true
			_, err = tx.Exec(ctx, "UPDATE media_deletions SET attempts=attempts+1,not_before=$2 WHERE object_key=$1", key, now.Add(time.Minute))
		} else {
			_, err = tx.Exec(ctx, "DELETE FROM media_deletions WHERE object_key=$1", key)
		}
		if err != nil {
			tx.Rollback(ctx)
			return ErrUnavailable
		}
		if err = tx.Commit(ctx); err != nil {
			tx.Rollback(ctx)
			return ErrUnavailable
		}
	}
	if failed {
		return ErrUnavailable
	}
	return nil
}
