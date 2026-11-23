package quests

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Every review-related read uses the same database deadline, including when
// the media worker has not run yet. Accepted photos follow the visit rules.
const photoInRetention = "(a.photo_expires_at IS NULL OR a.photo_expires_at>statement_timestamp())"

const expiredPending = "(a.status='pending_review' AND a.photo_expires_at<=statement_timestamp())"

const retireDuePhotos = `
	), expired_attempts AS (
		UPDATE quest_attempts a SET status='expired',revision=revision+1
		FROM due WHERE a.id=due.id AND a.status='pending_review'
		RETURNING a.id
	)
	UPDATE visit_photos f SET deleted_at=statement_timestamp()
	FROM due WHERE f.id=due.photo_id AND f.deleted_at IS NULL`

// ExpirePhotos retires a bounded batch and queues its objects in the same
// transaction. Locks are taken on attempts before their photos, as in review.
func (p Postgres) ExpirePhotos(ctx context.Context) error {
	_, err := p.Pool.Exec(ctx, `WITH due AS MATERIALIZED (
		SELECT a.id,a.photo_id FROM quest_attempts a
		JOIN visit_photos f ON f.id=a.photo_id
		WHERE a.status IN ('pending_review','rejected')
			AND a.photo_expires_at<=statement_timestamp()
			AND (f.deleted_at IS NULL OR a.status='pending_review')
		ORDER BY a.photo_expires_at,a.id LIMIT 25 FOR UPDATE OF a SKIP LOCKED`+retireDuePhotos)
	return err
}

// Do not make a returning participant wait for the background sweep before
// retrying an expired submission.
func expireCurrentAttempt(ctx context.Context, tx pgx.Tx, participation string, position int) error {
	_, err := tx.Exec(ctx, `WITH due AS MATERIALIZED (
		SELECT a.id,a.photo_id FROM quest_attempts a WHERE a.participation_id=$1
			AND a.position=$2 AND `+expiredPending+` FOR UPDATE OF a`+retireDuePhotos, participation, position)
	return err
}
