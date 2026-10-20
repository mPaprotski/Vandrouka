package visits

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrivateFieldsNeverSerialized(t *testing.T) {
	for _, value := range []any{
		Photo{ID: "photo-id", UserID: 123, ObjectKey: "private/object/key", SHA256: []byte("private-checksum")},
		Visit{ID: 1, UserID: 123, IdempotencyKey: "private-retry-key", RequestHash: []byte("private-fingerprint")},
	} {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"user_id", "object_key", "sha256", "request_hash", "idempotency_key", "upload_expires_at", "location_observed_at", "distance_m", "accuracy_m", "deleted_at", "private/", "private-retry"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("private field serialized: %s", secret)
			}
		}
	}
}
