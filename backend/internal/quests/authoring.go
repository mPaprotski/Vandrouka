package quests

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const questColumns = `id::text, city_id, title, description, status, revision, created_at, updated_at`

func scanQuest(row scanner) (Quest, error) {
	var q Quest
	err := row.Scan(&q.ID, &q.CityID, &q.Title, &q.Description, &q.Status, &q.Revision, &q.CreatedAt, &q.UpdatedAt)
	return q, notFound(err)
}

func (p Postgres) Create(ctx context.Context, owner, city int64) (Editor, error) {
	if city < 1 {
		return Editor{}, &Invalid{"city_id", "Выберите город"}
	}
	q, err := scanQuest(p.Pool.QueryRow(ctx, `INSERT INTO quests(owner_id,city_id)
		SELECT $1,id FROM cities WHERE id=$2 RETURNING `+questColumns, owner, city))
	return Editor{q, []Step{}}, err
}

func (p Postgres) List(ctx context.Context, owner int64, after string) (Page[Quest], error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+questColumns+` FROM quests
		WHERE owner_id=$1 AND id>$2 ORDER BY id LIMIT $3`, owner, after, PageSize+1)
	return readPage(rows, err, scanQuest, func(q Quest) string { return q.ID })
}

func loadEditor(ctx context.Context, tx pgx.Tx, owner int64, id string, write bool) (Editor, error) {
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	q, err := scanQuest(tx.QueryRow(ctx, "SELECT "+questColumns+" FROM quests WHERE id=$1 AND owner_id=$2"+lock, id, owner))
	if err != nil {
		return Editor{}, err
	}
	rows, err := tx.Query(ctx, `SELECT place_id,clue,photo_task,reveal_text FROM quest_steps
		WHERE quest_id=$1 ORDER BY position`, id)
	if err != nil {
		return Editor{}, err
	}
	steps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Step, error) {
		var s Step
		err := row.Scan(&s.PlaceID, &s.Clue, &s.PhotoTask, &s.RevealText)
		return s, err
	})
	if steps == nil {
		steps = []Step{}
	}
	return Editor{q, steps}, err
}

func (p Postgres) Get(ctx context.Context, owner int64, id string) (Editor, error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Editor, error) {
		return loadEditor(ctx, tx, owner, id, false)
	})
}

func validatePlaces(ctx context.Context, tx pgx.Tx, city int64, steps []Step) error {
	ids := make([]int64, len(steps))
	for i, s := range steps {
		ids[i] = s.PlaceID
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT id FROM places WHERE city_id=$1 AND id=ANY($2) AND is_active
		ORDER BY id FOR SHARE
	) AS matching`, city, ids).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(ids) {
		return &Invalid{"steps", "Выберите действующие места из каталога этого города"}
	}
	return nil
}

func (p Postgres) Save(ctx context.Context, owner int64, id string, input SaveInput) (Editor, error) {
	draft, err := validateDraft(input)
	if err != nil {
		return Editor{}, err
	}
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Editor, error) {
		current, err := lockQuest(ctx, tx, owner, id, input.ExpectedRevision)
		if err != nil {
			return Editor{}, err
		}
		if current.Status == "archived" {
			return Editor{}, ErrState
		}
		if err := validatePlaces(ctx, tx, current.CityID, draft.Steps); err != nil {
			return Editor{}, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM quest_steps WHERE quest_id=$1", id); err != nil {
			return Editor{}, err
		}
		for i, s := range draft.Steps {
			if _, err := tx.Exec(ctx, `INSERT INTO quest_steps(quest_id,position,place_id,clue,photo_task,reveal_text)
				VALUES($1,$2,$3,$4,$5,$6)`, id, i+1, s.PlaceID, s.Clue, s.PhotoTask, s.RevealText); err != nil {
				return Editor{}, err
			}
		}
		draft.Quest, err = scanQuest(tx.QueryRow(ctx, `UPDATE quests SET title=$2, description=$3, status='draft',
			revision=revision+1, updated_at=NOW() WHERE id=$1 RETURNING `+questColumns, id, draft.Title, draft.Description))
		return draft, err
	})
}

func lockQuest(ctx context.Context, tx pgx.Tx, owner int64, id string, revision int64) (Quest, error) {
	q, err := scanQuest(tx.QueryRow(ctx, "SELECT "+questColumns+" FROM quests WHERE id=$1 AND owner_id=$2 FOR UPDATE", id, owner))
	if err == nil && q.Revision != revision {
		err = ErrRevision
	}
	return q, err
}

func (p Postgres) Publish(ctx context.Context, owner int64, id string, revision int64) (Quest, error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Quest, error) {
		editor, err := loadEditor(ctx, tx, owner, id, true)
		if err != nil {
			return Quest{}, err
		}
		if editor.Revision != revision {
			return Quest{}, ErrRevision
		}
		if editor.Status != "draft" {
			return Quest{}, ErrState
		}
		if err = validatePublication(editor); err != nil {
			return Quest{}, err
		}
		if err = validatePlaces(ctx, tx, editor.CityID, editor.Steps); err != nil {
			return Quest{}, err
		}
		return scanQuest(tx.QueryRow(ctx, `UPDATE quests SET status='published', revision=revision+1,
			updated_at=NOW() WHERE id=$1 RETURNING `+questColumns, id))
	})
}

func (p Postgres) Archive(ctx context.Context, owner int64, id string, revision int64) (Quest, error) {
	return inTx(ctx, p.Pool, func(tx pgx.Tx) (Quest, error) {
		q, err := lockQuest(ctx, tx, owner, id, revision)
		if err != nil {
			return Quest{}, err
		}
		if q.Status == "archived" {
			return q, nil
		}
		return scanQuest(tx.QueryRow(ctx, `UPDATE quests SET status='archived', revision=revision+1,
			updated_at=NOW() WHERE id=$1 RETURNING `+questColumns, id))
	})
}

func (p Postgres) Delete(ctx context.Context, owner int64, id string, revision int64) error {
	_, err := inTx(ctx, p.Pool, func(tx pgx.Tx) (struct{}, error) {
		q, err := lockQuest(ctx, tx, owner, id, revision)
		if err != nil {
			return struct{}{}, err
		}
		if q.Status != "draft" {
			return struct{}{}, ErrState
		}
		tag, err := tx.Exec(ctx, `DELETE FROM quests WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM quest_runs WHERE quest_id=$1)`, id)
		if err == nil && tag.RowsAffected() == 0 {
			err = ErrState
		}
		return struct{}{}, err
	})
	return err
}
