package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRunRequestHashIsStableAndPayloadSensitive(t *testing.T) {
	t.Parallel()
	workflowID := uuid.New()
	deadline := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	base := SubmitRunParams{WorkflowID: workflowID, Input: json.RawMessage(`{"topic":"durability"}`), Deadline: &deadline}
	first, err := runRequestHash(base)
	if err != nil {
		t.Fatalf("runRequestHash() error = %v", err)
	}
	second, _ := runRequestHash(base)
	if first != second {
		t.Fatalf("hash is not stable: %q != %q", first, second)
	}
	base.Input = json.RawMessage(`{"topic":"different"}`)
	changed, _ := runRequestHash(base)
	if changed == first {
		t.Fatal("different payload produced the same hash")
	}
}

func TestNormalizeJSONObjectCanonicalizesFieldOrder(t *testing.T) {
	t.Parallel()
	first, err := normalizeJSONObject(json.RawMessage(` { "b": 2, "a": 1 } `))
	if err != nil {
		t.Fatalf("normalizeJSONObject() error = %v", err)
	}
	second, err := normalizeJSONObject(json.RawMessage(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("normalizeJSONObject() error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("normalized JSON differs: %s != %s", first, second)
	}
	if _, err := normalizeJSONObject(json.RawMessage(`[]`)); err == nil {
		t.Fatal("array accepted as run input")
	}
}
