//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

type workflowResponse struct {
	ID string `json:"id"`
}

type runResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type nodeResponse struct {
	NodeKey string `json:"node_key"`
	Status  string `json:"status"`
}

type eventResponse struct {
	Sequence  int64  `json:"sequence"`
	EventType string `json:"event_type"`
}

func TestDurableVerticalSlice(t *testing.T) {
	baseURL := os.Getenv("RELAY_INTEGRATION_BASE_URL")
	if baseURL == "" {
		t.Skip("RELAY_INTEGRATION_BASE_URL is not set")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	workflowBody, err := json.Marshal(map[string]any{
		"name":        "integration-" + suffix,
		"description": "real postgres and nats integration",
		"definition": map[string]any{"nodes": []any{
			map[string]any{"id": "left", "type": "llm", "inputs": map[string]any{"prompt": map[string]any{"from": "$.input.prompt"}}},
			map[string]any{"id": "right", "type": "transform", "inputs": map[string]any{"value": map[string]any{"from": "$.input.prompt"}}},
			map[string]any{"id": "finish", "type": "join", "depends_on": []string{"left", "right"}, "inputs": map[string]any{
				"left":  map[string]any{"from": "$.nodes.left.output"},
				"right": map[string]any{"from": "$.nodes.right.output"},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var workflow workflowResponse
	requestJSON(t, client, http.MethodPost, baseURL+"/workflows/", workflowBody, nil, http.StatusCreated, &workflow)
	if workflow.ID == "" {
		t.Fatal("workflow id is empty")
	}

	headers := map[string]string{"Idempotency-Key": "integration-" + suffix}
	var submitted runResponse
	requestJSON(t, client, http.MethodPost, baseURL+"/workflows/"+workflow.ID+"/runs", []byte(`{"input":{"prompt":"integration"}}`), headers, http.StatusAccepted, &submitted)
	var replayed runResponse
	requestJSON(t, client, http.MethodPost, baseURL+"/workflows/"+workflow.ID+"/runs", []byte(`{"input":{"prompt":"integration"}}`), headers, http.StatusAccepted, &replayed)
	if replayed.ID != submitted.ID {
		t.Fatalf("idempotent replay run = %q, want %q", replayed.ID, submitted.ID)
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/workflows/"+workflow.ID+"/runs", []byte(`{"input":{"prompt":"different"}}`), headers, http.StatusConflict, nil)

	deadline := time.Now().Add(20 * time.Second)
	var run runResponse
	for {
		requestJSON(t, client, http.MethodGet, baseURL+"/runs/"+submitted.ID+"/", nil, nil, http.StatusOK, &run)
		if run.Status == "succeeded" || run.Status == "failed" || run.Status == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not finish before deadline; last status %q", run.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if run.Status != "succeeded" {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}

	var nodes struct {
		Items []nodeResponse `json:"items"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/runs/"+run.ID+"/nodes", nil, nil, http.StatusOK, &nodes)
	if len(nodes.Items) != 3 {
		t.Fatalf("node count = %d, want 3", len(nodes.Items))
	}
	for _, node := range nodes.Items {
		if node.Status != "succeeded" {
			t.Errorf("node %s status = %q", node.NodeKey, node.Status)
		}
	}

	var events struct {
		Items []eventResponse `json:"items"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/runs/"+run.ID+"/events?after=0", nil, nil, http.StatusOK, &events)
	if len(events.Items) < 8 {
		t.Fatalf("event count = %d, want at least 8", len(events.Items))
	}
	for index, event := range events.Items {
		want := int64(index + 1)
		if event.Sequence != want {
			t.Fatalf("event sequence[%d] = %d, want %d", index, event.Sequence, want)
		}
	}
	if last := events.Items[len(events.Items)-1].EventType; last != "run.succeeded" {
		t.Fatalf("last event = %q, want run.succeeded", last)
	}
	resumeAfter := events.Items[len(events.Items)/2].Sequence
	var replayedEvents struct {
		Items []eventResponse `json:"items"`
	}
	requestJSON(t, client, http.MethodGet, fmt.Sprintf("%s/runs/%s/events?after=%d", baseURL, run.ID, resumeAfter), nil, nil, http.StatusOK, &replayedEvents)
	if len(replayedEvents.Items) == 0 || replayedEvents.Items[0].Sequence != resumeAfter+1 {
		t.Fatalf("event replay after %d = %+v", resumeAfter, replayedEvents.Items)
	}

	var usage struct {
		Items []json.RawMessage `json:"items"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/runs/"+run.ID+"/usage", nil, nil, http.StatusOK, &usage)
	if len(usage.Items) != 1 {
		t.Fatalf("usage record count = %d, want 1 for the LLM node", len(usage.Items))
	}
}

func requestJSON(t *testing.T, client *http.Client, method, url string, body []byte, headers map[string]string, wantStatus int, destination any) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, url, response.StatusCode, wantStatus, payload)
	}
	if destination != nil {
		if err := json.Unmarshal(payload, destination); err != nil {
			t.Fatalf("decode %s %s: %v: %s", method, url, err, payload)
		}
	}
}
