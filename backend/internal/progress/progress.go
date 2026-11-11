// Package progress calculates lifetime XP levels from the immutable reward ledger.
package progress

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var thresholds = [...]int64{0, 100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500}

type State struct {
	TotalXP       int64  `json:"total_xp"`
	Level         int    `json:"level"`
	LevelStartXP  int64  `json:"level_start_xp"`
	NextLevelXP   *int64 `json:"next_level_xp"`
	XPToNextLevel *int64 `json:"xp_to_next_level"`
}

func Calculate(xp int64) (State, error) {
	if xp < 0 {
		return State{}, errors.New("negative XP")
	}
	level := 1
	for i, threshold := range thresholds {
		if xp >= threshold {
			level = i + 1
		} else {
			break
		}
	}
	state := State{TotalXP: xp, Level: level, LevelStartXP: thresholds[level-1]}
	if level < len(thresholds) {
		next := thresholds[level]
		remaining := next - xp
		state.NextLevelXP = &next
		state.XPToNextLevel = &remaining
	}
	return state, nil
}

type Postgres struct{ Pool *pgxpool.Pool }

func (p Postgres) Read(ctx context.Context, owner int64) (State, error) {
	if owner < 1 {
		return State{}, errors.New("invalid progress owner")
	}
	var xp int64
	// One statement snapshot: no visit/photo joins, so hiding history does not revoke XP.
	err := p.Pool.QueryRow(ctx, "SELECT COALESCE(SUM(amount),0) FROM xp_transactions WHERE user_id=$1", owner).Scan(&xp)
	if err != nil {
		return State{}, err
	}
	return Calculate(xp)
}
