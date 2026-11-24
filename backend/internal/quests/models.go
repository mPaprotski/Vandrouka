// Package quests implements authoring and invitation-only photo quests.
package quests

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound = errors.New("quest resource not found")
	ErrRevision = errors.New("quest revision conflict")
	ErrState    = errors.New("quest action not available in current state")
	ErrInvite   = errors.New("quest invitation unavailable")
)

type Invalid struct {
	Field   string
	Message string
}

func (e *Invalid) Error() string { return e.Field + ": " + e.Message }

type Quest struct {
	ID          string    `json:"id"`
	CityID      int64     `json:"city_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Revision    int64     `json:"revision"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Step struct {
	PlaceID    int64  `json:"place_id"`
	Clue       string `json:"clue"`
	PhotoTask  string `json:"photo_task"`
	RevealText string `json:"reveal_text"`
}

type Editor struct {
	Quest
	Steps []Step `json:"steps"`
}

type SaveInput struct {
	ExpectedRevision int64   `json:"expected_revision"`
	Title            string  `json:"title"`
	Description      string  `json:"description"`
	Steps            *[]Step `json:"steps"`
}

type Run struct {
	ID            string     `json:"id"`
	QuestID       string     `json:"quest_id"`
	QuestRevision int64      `json:"quest_revision"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	JoinEnabled   bool       `json:"join_enabled"`
	TotalSteps    int        `json:"total_steps"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type Invite struct {
	Run
	JoinCode string `json:"join_code"`
}

type Task struct {
	ID        string `json:"task_id"`
	Position  int    `json:"position"`
	Clue      string `json:"clue"`
	PhotoTask string `json:"photo_task"`
}

type Participation struct {
	ID             string    `json:"id"`
	RunID          string    `json:"run_id"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	CompletedSteps int       `json:"completed_steps"`
	TotalSteps     int       `json:"total_steps"`
	JoinedAt       time.Time `json:"joined_at"`
}

type Progress struct {
	Participation
	CurrentTask    *Task      `json:"current_task,omitempty"`
	CurrentAttempt *Attempt   `json:"current_attempt,omitempty"`
	Stories        []Story    `json:"stories"`
	XPAwarded      int        `json:"xp_awarded"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	ReviewPending  bool       `json:"review_pending"`
}

type Participant struct {
	ID             string    `json:"id"`
	DisplayName    string    `json:"display_name"`
	CompletedSteps int       `json:"completed_steps"`
	JoinedAt       time.Time `json:"joined_at"`
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func cleanText(raw, field string, min, max int) (string, error) {
	value := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(value)
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || length < min || length > max {
		return "", &Invalid{field, fmt.Sprintf("Длина поля должна быть от %d до %d символов", min, max)}
	}
	return value, nil
}

func validateDraft(input SaveInput) (Editor, error) {
	var result Editor
	if input.Steps == nil || len(*input.Steps) > 20 {
		return result, &Invalid{"steps", "Передайте список из 0–20 заданий"}
	}
	var err error
	if result.Title, err = cleanText(input.Title, "title", 0, 120); err != nil {
		return result, err
	}
	if result.Description, err = cleanText(input.Description, "description", 0, 3000); err != nil {
		return result, err
	}
	seen := make(map[int64]bool)
	result.Steps = make([]Step, len(*input.Steps))
	for i, step := range *input.Steps {
		prefix := fmt.Sprintf("steps[%d].", i)
		if step.PlaceID < 1 || seen[step.PlaceID] {
			return result, &Invalid{prefix + "place_id", "Для каждого задания выберите отдельное место"}
		}
		seen[step.PlaceID] = true
		for _, field := range []struct {
			name  string
			value *string
			limit int
		}{{"clue", &step.Clue, 1000}, {"photo_task", &step.PhotoTask, 1000}, {"reveal_text", &step.RevealText, 3000}} {
			*field.value, err = cleanText(*field.value, prefix+field.name, 0, field.limit)
			if err != nil {
				return result, err
			}
		}
		result.Steps[i] = step
	}
	return result, nil
}

func validatePublication(editor Editor) error {
	if utf8.RuneCountInString(editor.Title) < 3 {
		return &Invalid{"title", "Введите название из 3–120 символов"}
	}
	if len(editor.Steps) < 2 {
		return &Invalid{"steps", "Добавьте хотя бы два задания"}
	}
	for i, step := range editor.Steps {
		for _, field := range []struct {
			name, value string
		}{{"clue", step.Clue}, {"photo_task", step.PhotoTask}, {"reveal_text", step.RevealText}} {
			if strings.TrimSpace(field.value) == "" {
				return &Invalid{fmt.Sprintf("steps[%d].%s", i, field.name), "Заполните это поле перед публикацией"}
			}
		}
	}
	return nil
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	var code [8]byte
	_, _ = rand.Read(code[:])
	for i, b := range code {
		code[i] = codeAlphabet[b&31]
	}
	return string(code[:])
}

func inviteHash(raw string) ([32]byte, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if len(code) != 8 {
		return [32]byte{}, ErrInvite
	}
	for _, ch := range code {
		if !strings.ContainsRune(codeAlphabet, ch) {
			return [32]byte{}, ErrInvite
		}
	}
	return sha256.Sum256([]byte(code)), nil
}
