package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type runResult struct {
	duration time.Duration
	retries  int
	ok       bool
}

type resourceSample struct {
	rssBytes float64
	cpu      float64
}

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:8080/api/v1", "Relay API base URL")
	runsPerLevel := flag.Int("runs", 20, "runs submitted at each concurrency level")
	timeout := flag.Duration("timeout", 30*time.Second, "timeout for each run")
	flag.Parse()
	if *runsPerLevel < 1 {
		fmt.Fprintln(os.Stderr, "-runs must be positive")
		os.Exit(2)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	workflowID, err := createWorkflow(client, *baseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("| Concurrency | Runs | Throughput | p50 | p95 | Completion | Retries | API RSS | API CPU delta |")
	fmt.Println("| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, concurrency := range []int{1, 5, 10, 25} {
		before := sampleResources(client, *baseURL)
		started := time.Now()
		results := executeLevel(client, *baseURL, workflowID, concurrency, *runsPerLevel, *timeout)
		elapsed := time.Since(started)
		after := sampleResources(client, *baseURL)
		durations := make([]time.Duration, 0, len(results))
		completed, retries := 0, 0
		for _, result := range results {
			if result.ok {
				completed++
				durations = append(durations, result.duration)
			}
			retries += result.retries
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p50, p95 := percentile(durations, 0.50), percentile(durations, 0.95)
		throughput := float64(completed) / elapsed.Seconds()
		fmt.Println(fmt.Sprintf("| %d | %d | %.2f runs/s | %s | %s | %.1f%% | %d | %.1f MiB | %.3fs |",
			concurrency, len(results), throughput, p50.Round(time.Millisecond), p95.Round(time.Millisecond),
			100*float64(completed)/float64(len(results)), retries, after.rssBytes/(1024*1024), after.cpu-before.cpu))
	}
}

func createWorkflow(client *http.Client, baseURL string) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"name":        fmt.Sprintf("benchmark-%d", time.Now().UnixNano()),
		"description": "deterministic load harness",
		"definition": map[string]any{"nodes": []any{
			map[string]any{"id": "execute", "type": "llm", "inputs": map[string]any{"prompt": map[string]any{"from": "$.input.prompt"}}},
		}},
	})
	var response struct {
		ID string `json:"id"`
	}
	if err := requestJSON(client, http.MethodPost, baseURL+"/workflows/", payload, nil, http.StatusCreated, &response); err != nil {
		return "", fmt.Errorf("create benchmark workflow: %w", err)
	}
	return response.ID, nil
}

func executeLevel(client *http.Client, baseURL, workflowID string, concurrency, count int, timeout time.Duration) []runResult {
	results := make([]runResult, count)
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = executeRun(client, baseURL, workflowID, index, timeout)
			}
		}()
	}
	for index := range count {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func executeRun(client *http.Client, baseURL, workflowID string, index int, timeout time.Duration) runResult {
	started := time.Now()
	payload, _ := json.Marshal(map[string]any{"input": map[string]any{"prompt": fmt.Sprintf("benchmark-%d", index)}})
	headers := map[string]string{"Idempotency-Key": fmt.Sprintf("bench-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&sequence, 1))}
	var submitted struct {
		ID string `json:"id"`
	}
	if err := requestJSON(client, http.MethodPost, baseURL+"/workflows/"+workflowID+"/runs", payload, headers, http.StatusAccepted, &submitted); err != nil {
		return runResult{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		var current struct {
			Status string `json:"status"`
		}
		if err := requestJSON(client, http.MethodGet, baseURL+"/runs/"+submitted.ID+"/", nil, nil, http.StatusOK, &current); err != nil {
			return runResult{}
		}
		if current.Status == "succeeded" {
			return runResult{duration: time.Since(started), retries: runRetries(client, baseURL, submitted.ID), ok: true}
		}
		if current.Status == "failed" || current.Status == "cancelled" {
			return runResult{duration: time.Since(started), retries: runRetries(client, baseURL, submitted.ID)}
		}
		select {
		case <-ctx.Done():
			return runResult{}
		case <-time.After(25 * time.Millisecond):
		}
	}
}

var sequence uint64

func runRetries(client *http.Client, baseURL, runID string) int {
	var response struct {
		Items []struct {
			Attempt int `json:"current_attempt_no"`
		} `json:"items"`
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/runs/"+runID+"/nodes", nil, nil, http.StatusOK, &response); err != nil {
		return 0
	}
	retries := 0
	for _, node := range response.Items {
		if node.Attempt > 1 {
			retries += node.Attempt - 1
		}
	}
	return retries
}

func requestJSON(client *http.Client, method, url string, body []byte, headers map[string]string, wantStatus int, destination any) error {
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return err
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
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("%s %s returned %d: %s", method, url, response.StatusCode, payload)
	}
	if destination != nil {
		return json.Unmarshal(payload, destination)
	}
	return nil
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * quantile)
	return values[index]
}

func sampleResources(client *http.Client, baseURL string) resourceSample {
	metricsURL := strings.TrimSuffix(strings.Split(baseURL, "/api/v1")[0], "/") + "/metrics"
	response, err := client.Get(metricsURL)
	if err != nil {
		return resourceSample{}
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	var sample resourceSample
	for _, line := range strings.Split(string(payload), string(rune(10))) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "process_resident_memory_bytes":
			sample.rssBytes = value
		case "process_cpu_seconds_total":
			sample.cpu = value
		}
	}
	return sample
}
