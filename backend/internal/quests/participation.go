package quests

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const participationColumns = `p.id::text,r.id::text,r.title,
	CASE WHEN p.completed_at IS NOT NULL THEN 'completed' WHEN r.finished_at IS NOT NULL THEN 'closed' WHEN r.started_at IS NULL THEN 'waiting' ELSE 'active' END,
	COALESCE(p.current_position-1,summary.total),summary.total,p.joined_at`

const stepCount = ` CROSS JOIN LATERAL (SELECT count(*) AS total FROM quest_run_steps WHERE run_id=r.id) summary `

func scanParticipation(row scanner) (Participation, error) {
	var p Participation
	err := row.Scan(&p.ID, &p.RunID, &p.Title, &p.Status, &p.CompletedSteps, &p.TotalSteps, &p.JoinedAt)
	return p, notFound(err)
}

func participationProgress(ctx context.Context, db queryRow, actor int64, id string) (Progress, error) {
	// Each projection is scoped by the participation. Current task omits the
	// answer, and stories are emitted only for committed acceptances.
	row := db.QueryRow(ctx, `SELECT `+participationColumns+`, step.id::text,step.position,step.clue,step.photo_task,
		CASE WHEN r.started_at IS NOT NULL THEN GREATEST(r.started_at,p.joined_at) END,
		p.completed_at,latest.item,stories.items,stories.xp,
		EXISTS (SELECT 1 FROM quest_attempts a WHERE a.participation_id=p.id
			AND a.position=p.current_position AND a.status='pending_review' AND `+photoInRetention+`)
		FROM quest_participations p JOIN quest_runs r ON r.id=p.run_id`+stepCount+`
		LEFT JOIN quest_run_steps step ON step.run_id=r.id
			AND step.position=p.current_position AND r.started_at IS NOT NULL AND r.finished_at IS NULL
		LEFT JOIN LATERAL (
			SELECT jsonb_strip_nulls(to_jsonb(item)) AS item FROM (
				SELECT `+attemptColumns+attemptFrom+`
				WHERE a.participation_id=p.id AND a.position=step.position AND a.status<>'withdrawn'
				ORDER BY (a.status='draft' OR (a.status='pending_review' AND `+photoInRetention+`)) DESC,a.created_at DESC,a.id DESC LIMIT 1
			) item
		) latest ON true
		CROSS JOIN LATERAL (
			SELECT COALESCE(jsonb_agg(jsonb_build_object(
				'task_id',s.id,'position',s.position,'place_id',s.place_id,'place_name',s.place_name,
				'address',s.address,'text',s.reveal_text,'links',s.links,
				'attempt_id',a.id,'xp_awarded',a.xp_awarded
			) ORDER BY s.position),'[]'::jsonb) AS items,COALESCE(sum(a.xp_awarded),0) AS xp
			FROM quest_attempts a JOIN quest_run_steps s ON s.run_id=a.run_id AND s.position=a.position
			WHERE a.participation_id=p.id AND a.status='accepted'
		) stories WHERE p.user_id=$1 AND p.id=$2`, actor, id)

	var result Progress
	var taskID, clue, photoTask *string
	var position *int
	err := row.Scan(&result.ID, &result.RunID, &result.Title, &result.Status, &result.CompletedSteps, &result.TotalSteps,
		&result.JoinedAt, &taskID, &position, &clue, &photoTask,
		&result.StartedAt, &result.CompletedAt, &result.CurrentAttempt, &result.Stories, &result.XPAwarded, &result.ReviewPending)
	if err != nil {
		return Progress{}, notFound(err)
	}
	if taskID != nil {
		result.CurrentTask = &Task{*taskID, *position, *clue, *photoTask}
	}
	return result, nil
}

func (p Postgres) Join(ctx context.Context, actor int64, rawCode, displayName string) (Progress, error) {
	hash, err := inviteHash(rawCode)
	if err != nil {
		return Progress{}, err
	}
	name, err := cleanText(displayName, "display_name", 1, 60)
	if err != nil {
		return Progress{}, err
	}
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Progress, error) {
		var user int64
		if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR KEY SHARE", actor).Scan(&user); err != nil {
			return Progress{}, notFound(err)
		}
		var run string
		err := tx.QueryRow(ctx, "SELECT id::text FROM quest_runs WHERE join_code_hash=$1 AND join_enabled FOR SHARE", hash[:]).Scan(&run)
		if errors.Is(err, pgx.ErrNoRows) {
			return Progress{}, ErrInvite
		}
		if err != nil {
			return Progress{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO quest_participations(run_id,user_id,display_name)
			VALUES($1,$2,$3) ON CONFLICT (run_id,user_id) DO NOTHING`, run, actor, name)
		if err != nil {
			return Progress{}, err
		}
		var id string
		err = tx.QueryRow(ctx, "SELECT id::text FROM quest_participations WHERE run_id=$1 AND user_id=$2", run, actor).Scan(&id)
		if err != nil {
			return Progress{}, err
		}
		return participationProgress(ctx, tx, actor, id)
	})
}

func (p Postgres) GetParticipation(ctx context.Context, actor int64, id string) (Progress, error) {
	return participationProgress(ctx, p.Pool, actor, id)
}

func (p Postgres) ListParticipations(ctx context.Context, actor int64, after string) (Page[Participation], error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+participationColumns+` FROM quest_participations p
		JOIN quest_runs r ON r.id=p.run_id`+stepCount+`WHERE p.user_id=$1 AND p.id>$2 ORDER BY p.id LIMIT $3`, actor, after, PageSize+1)
	return readPage(rows, err, scanParticipation, func(p Participation) string { return p.ID })
}

func scanParticipant(row scanner) (Participant, error) {
	var p Participant
	err := row.Scan(&p.ID, &p.DisplayName, &p.CompletedSteps, &p.JoinedAt)
	return p, err
}

func (p Postgres) ListParticipants(ctx context.Context, owner int64, runID, after string) (Page[Participant], error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Page[Participant], error) {
		if err := lockManagedRun(ctx, tx, owner, runID); err != nil {
			return Page[Participant]{}, err
		}
		rows, err := tx.Query(ctx, `SELECT id::text,display_name,COALESCE(current_position-1,
			(SELECT count(*) FROM quest_run_steps WHERE run_id=$1)),joined_at FROM quest_participations
			WHERE run_id=$1 AND id>$2 ORDER BY id LIMIT $3`, runID, after, PageSize+1)
		return readPage(rows, err, scanParticipant, func(p Participant) string { return p.ID })
	})
}

func lockManagedRun(ctx context.Context, tx pgx.Tx, owner int64, runID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT r.id::text FROM quest_runs r JOIN quests q ON q.id=r.quest_id
		WHERE r.id=$1 AND q.owner_id=$2 FOR KEY SHARE OF r`, runID, owner).Scan(&id)
	return notFound(err)
}
