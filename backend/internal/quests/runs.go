package quests

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/store"
)

const runColumns = `r.id::text,r.quest_id::text,r.quest_revision,r.title,r.join_enabled,
	r.created_at,r.started_at,r.finished_at,(SELECT count(*) FROM quest_run_steps WHERE run_id=r.id)`

func scanRun(row scanner) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.QuestID, &r.QuestRevision, &r.Title, &r.JoinEnabled, &r.CreatedAt, &r.StartedAt, &r.FinishedAt, &r.TotalSteps)
	r.Status = "waiting"
	if r.FinishedAt != nil {
		r.Status = "finished"
	} else if r.StartedAt != nil {
		r.Status = "active"
	}
	return r, notFound(err)
}

func (p Postgres) CreateRun(ctx context.Context, owner int64, questID string, revision int64, title string) (Invite, error) {
	title, err := cleanText(title, "title", 3, 120)
	if err != nil {
		return Invite{}, err
	}
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Invite, error) {
		editor, err := loadEditor(ctx, tx, owner, questID, false)
		if err != nil {
			return Invite{}, err
		}
		if editor.Revision != revision {
			return Invite{}, ErrRevision
		}
		if editor.Status != "published" {
			return Invite{}, ErrState
		}
		if err := validatePlaces(ctx, tx, editor.CityID, editor.Steps); err != nil {
			return Invite{}, err
		}

		id, err := uuid.NewRandom()
		if err != nil {
			return Invite{}, err
		}
		var code string
		for {
			code = newCode()
			hash, _ := inviteHash(code)
			tag, err := tx.Exec(ctx, `INSERT INTO quest_runs(id,quest_id,quest_revision,title,join_code_hash)
				VALUES($1,$2,$3,$4,$5) ON CONFLICT (join_code_hash) DO NOTHING`, id.String(), questID, revision, title, hash[:])
			if err != nil {
				return Invite{}, err
			}
			if tag.RowsAffected() != 0 {
				break
			}
		}
		catalog := p.Catalog(tx)
		for i, step := range editor.Steps {
			place, err := catalog.GetPlace(ctx, step.PlaceID)
			if errors.Is(err, store.ErrNotFound) {
				return Invite{}, &Invalid{"steps", "Одно из мест больше недоступно"}
			}
			if err != nil {
				return Invite{}, err
			}
			if place.Links == nil {
				place.Links = []places.Link{}
			}
			_, err = tx.Exec(ctx, `INSERT INTO quest_run_steps(run_id,position,place_id,place_name,address,lat,lon,links,clue,photo_task,reveal_text)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id.String(), i+1, step.PlaceID, place.Name, place.Address,
				place.Lat, place.Lon, place.Links, step.Clue, step.PhotoTask, step.RevealText)
			if err != nil {
				return Invite{}, err
			}
		}
		run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM quest_runs r WHERE r.id=$1", id.String()))
		return Invite{run, code}, err
	})
}

func (p Postgres) GetRun(ctx context.Context, owner int64, id string) (Run, error) {
	return scanRun(p.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM quest_runs r JOIN quests q ON q.id=r.quest_id
		WHERE r.id=$1 AND q.owner_id=$2`, id, owner))
}

func (p Postgres) ListRuns(ctx context.Context, owner int64, after string) (Page[Run], error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+runColumns+` FROM quest_runs r JOIN quests q ON q.id=r.quest_id
		WHERE q.owner_id=$1 AND r.id>$2 ORDER BY r.id LIMIT $3`, owner, after, PageSize+1)
	return readPage(rows, err, scanRun, func(r Run) string { return r.ID })
}

func (p Postgres) Start(ctx context.Context, owner int64, id string) (Run, error) {
	return scanRun(p.Pool.QueryRow(ctx, `UPDATE quest_runs r SET started_at=COALESCE(r.started_at,NOW())
		FROM quests q WHERE r.id=$1 AND q.id=r.quest_id AND q.owner_id=$2 RETURNING `+runColumns, id, owner))
}

func (p Postgres) Finish(ctx context.Context, owner int64, id string) (Run, error) {
	run, err := scanRun(p.Pool.QueryRow(ctx, `UPDATE quest_runs r
		SET finished_at=COALESCE(r.finished_at,GREATEST(r.started_at, clock_timestamp())), join_enabled=false
		FROM quests q WHERE r.id=$1 AND q.id=r.quest_id AND q.owner_id=$2 AND r.started_at IS NOT NULL
		RETURNING `+runColumns, id, owner))
	if errors.Is(err, ErrNotFound) {
		if _, lookupErr := p.GetRun(ctx, owner, id); lookupErr != nil {
			return Run{}, lookupErr
		}
		return Run{}, ErrState
	}
	return run, err
}

func (p Postgres) SetJoining(ctx context.Context, owner int64, id string, enabled bool) (Run, error) {
	run, err := scanRun(p.Pool.QueryRow(ctx, `UPDATE quest_runs r SET join_enabled=$3 FROM quests q
		WHERE r.id=$1 AND q.id=r.quest_id AND q.owner_id=$2 AND r.finished_at IS NULL
		RETURNING `+runColumns, id, owner, enabled))
	if errors.Is(err, ErrNotFound) {
		if _, lookupErr := p.GetRun(ctx, owner, id); lookupErr != nil {
			return Run{}, lookupErr
		}
		return Run{}, ErrState
	}
	return run, err
}

func (p Postgres) RotateCode(ctx context.Context, owner int64, id string) (Invite, error) {
	for {
		code := newCode()
		hash, _ := inviteHash(code)
		// Treat a randomly generated copy of the old code as a retry too.
		next, err := scanRun(p.Pool.QueryRow(ctx, `UPDATE quest_runs r SET join_code_hash=$3
			FROM quests q WHERE r.id=$1 AND q.id=r.quest_id AND q.owner_id=$2
			AND r.finished_at IS NULL AND r.join_code_hash<>$3 RETURNING `+runColumns, id, owner, hash[:]))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "quest_runs_join_code_hash_key" {
			continue
		}
		if errors.Is(err, ErrNotFound) {
			run, lookupErr := p.GetRun(ctx, owner, id)
			if lookupErr != nil {
				return Invite{}, lookupErr
			}
			if run.FinishedAt != nil {
				return Invite{}, ErrState
			}
			continue
		}
		return Invite{next, code}, err
	}
}
