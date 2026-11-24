package quests

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

func (p Postgres) ReviewQueue(ctx context.Context, organizer int64, runID, after string) (Page[ReviewItem], error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Page[ReviewItem], error) {
		if err := lockManagedRun(ctx, tx, organizer, runID); err != nil {
			return Page[ReviewItem]{}, err
		}
		rows, err := tx.Query(ctx, "SELECT "+attemptColumns+`,p.display_name,s.place_name,s.photo_task,a.distance_m,a.accuracy_m`+
			attemptFrom+` JOIN quest_participations p ON p.id=a.participation_id
			WHERE a.run_id=$1 AND a.id>$2 AND a.status='pending_review'
			AND f.status='ready' AND f.deleted_at IS NULL AND `+photoInRetention+` ORDER BY a.id LIMIT $3`, runID, after, PageSize+1)
		return readPage(rows, err, scanReviewItem, func(item ReviewItem) string { return item.ID })
	})
}

func scanReviewItem(row scanner) (ReviewItem, error) {
	var item ReviewItem
	err := row.Scan(append(attemptFields(&item.Attempt), &item.ParticipantName, &item.PlaceName, &item.PhotoTask,
		&item.DistanceM, &item.AccuracyM)...)
	return item, err
}

func (p Postgres) Review(ctx context.Context, organizer int64, id string, input ReviewInput) (Attempt, error) {
	switch input.Decision {
	case "accept":
		if input.Reason != "" {
			return Attempt{}, &Invalid{"reason", "Комментарий нужен только при отклонении"}
		}
	case "reject":
		var err error
		input.Reason, err = cleanText(input.Reason, "reason", 1, 1000)
		if err != nil {
			return Attempt{}, err
		}
	default:
		return Attempt{}, &Invalid{"decision", "Выберите accept или reject"}
	}

	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Attempt, error) {
		var owner int64
		err := tx.QueryRow(ctx, `SELECT a.user_id FROM quest_attempts a JOIN quest_runs r ON r.id=a.run_id
			JOIN quests q ON q.id=r.quest_id WHERE a.id=$1 AND q.owner_id=$2 AND a.submitted_at IS NOT NULL
			AND a.status<>'withdrawn'`, id, organizer).Scan(&owner)
		if err != nil {
			return Attempt{}, notFound(err)
		}
		if err = lockAccount(ctx, tx, owner); err != nil {
			return Attempt{}, err
		}
		a, err := lockAttempt(ctx, tx, owner, id)
		if err != nil {
			return Attempt{}, err
		}
		if a.Status == withdrawn {
			return Attempt{}, ErrNotFound
		}
		if a.Revision-1 == input.ExpectedRevision && ((input.Decision == "accept" && a.Status == accepted) ||
			(input.Decision == "reject" && a.Status == rejected && a.Reason != nil && *a.Reason == input.Reason)) {
			return getAttempt(ctx, tx, owner, id)
		}
		if a.Status == "expired" || (a.Status == pendingReview && a.PhotoExpired) {
			return Attempt{}, ErrPhotoExpired
		}
		if a.Revision != input.ExpectedRevision {
			return Attempt{}, ErrRevision
		}
		if a.Status != pendingReview || !a.isCurrent() {
			return Attempt{}, ErrState
		}
		if err = requireReadyPhoto(ctx, tx, a.PhotoID); err != nil {
			return Attempt{}, err
		}
		now := time.Now().UTC()
		if input.Decision == "reject" {
			_, err = tx.Exec(ctx, `UPDATE quest_attempts SET status='rejected',revision=revision+1,
				reviewed_at=$2,rejection_reason=$3,photo_expires_at=clock_timestamp()+INTERVAL '24 hours' WHERE id=$1`, id, now, input.Reason)
		} else {
			err = acceptAttempt(ctx, tx, owner, id, a, now)
		}
		if err != nil {
			return Attempt{}, err
		}
		return getAttempt(ctx, tx, owner, id)
	})
}

func acceptAttempt(ctx context.Context, tx pgx.Tx, owner int64, id string, a lockedAttempt, now time.Time) error {
	// GPS was validated at submission. Use the stored evidence and preserve
	// the ordinary first-visit reward ledger inside this transaction.
	result, err := visits.RecordQuestVisit(ctx, tx, visits.QuestEvidence{
		Owner: owner, Place: a.PlaceID, PhotoID: *a.PhotoID, RequestHash: a.SubmissionHash,
		ObservedAt: *a.ObservedAt, DistanceM: *a.Distance, AccuracyM: *a.Accuracy,
	}, now)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE quest_attempts SET status='accepted',revision=revision+1,
		reviewed_at=$2,visit_id=$3,xp_awarded=$4,photo_expires_at=NULL WHERE id=$1`, id, now, result.Visit.ID, result.XPAwarded); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE quest_participations p SET
		current_position=n.position,completed_at=CASE WHEN n.position IS NULL THEN $2::timestamptz END
		FROM (SELECT min(position) AS position FROM quest_run_steps WHERE run_id=$3 AND position>$4) n
		WHERE p.id=$1`, a.ParticipationID, now, a.Part.RunID, a.Position)
	return err
}
