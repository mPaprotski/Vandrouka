package history

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCursor(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 34, 56, 123456000, time.UTC)
	got, err := decodeCursor(encodeCursor(Item{ID: 42, CreatedAt: at}))
	if err != nil || got.ID != 42 || !got.CreatedAt.Equal(at) {
		t.Fatal(got, err)
	}
	for _, raw := range []string{`{}`, `{"v":2,"id":1,"at":"2026-10-30T12:00:00Z"}`, `{"v":1,"id":0,"at":"2026-10-30T12:00:00Z"}`, `{"v":1,"id":1,"at":"bad"}`, `{"v":1,"id":1,"at":"2026-10-30T12:00:00Z","owner":2}`, `{"v":1,"id":1,"at":"2026-10-30T12:00:00Z"} {}`} {
		if _, err := decodeCursor(base64.RawURLEncoding.EncodeToString([]byte(raw))); !errors.Is(err, ErrInvalid) {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"", "!", strings.Repeat("a", 257), encodeCursor(Item{ID: 42, CreatedAt: at}) + "="} {
		if _, err := decodeCursor(raw); !errors.Is(err, ErrInvalid) {
			t.Fatal(raw, err)
		}
	}
}
