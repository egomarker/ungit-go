package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egomarker/ungit-go/internal/config"
	"github.com/egomarker/ungit-go/internal/observability"
)

func TestHTTPInstrumentationAndClientLogging(t *testing.T) {
	directory := t.TempDir()
	logging, err := observability.Start(observability.Options{
		Directory:  &directory,
		Level:      "trace",
		MaxSizeMB:  1,
		MaxBackups: 1,
		MaxAgeDays: 1,
		Version:    "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.RootPath = "/ungit"
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())

	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/ungit/api/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Request-ID", "external-request-1")
	request.Header.Set("X-Action-ID", "action-aabbccddeeff")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	expectedRequestID := "ext-" + observability.Fingerprint("external-request-1")
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Request-ID") != expectedRequestID || response.Header.Get("X-Action-ID") != "action-aabbccddeeff" {
		t.Fatalf("ping status=%d request-id=%q action-id=%q", response.StatusCode,
			response.Header.Get("X-Request-ID"), response.Header.Get("X-Action-ID"))
	}

	request, err = http.NewRequest(http.MethodGet, testServer.URL+"/api/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Request-ID", "req-112233aabbcc")
	request.Header.Set("X-Action-ID", "action-112233aabbcc")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.Header.Get("X-Request-ID") != "req-112233aabbcc" || response.Header.Get("X-Action-ID") != "action-112233aabbcc" {
		t.Fatalf("issued correlation IDs were not preserved: request-id=%q action-id=%q",
			response.Header.Get("X-Request-ID"), response.Header.Get("X-Action-ID"))
	}

	secret := "client-secret-canary"
	payload, _ := json.Marshal(map[string]any{
		"level":    "error",
		"event":    "browser.window_error",
		"message":  "failure token=" + secret,
		"stack":    "stack password=" + secret,
		"page":     "/#/repository?path=page-secret-canary",
		"socketId": "7",
		"details": map[string]any{
			"authorization": secret,
			"unknown":       "details-secret-canary",
			"path":          "/api/status",
			"requestId":     "req-0123456789ab",
			"actionId":      "action-0123456789ab",
		},
	})
	response, err = http.Post(testServer.URL+"/ungit/api/client-log", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("client log status=%d", response.StatusCode)
	}

	testServer.Close()
	if err := logging.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(directory, observability.LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seenStarted, seenCompleted, seenClient := false, false, false
	var all strings.Builder
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		all.Write(scanner.Bytes())
		all.WriteByte('\n')
		var entry map[string]any
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		switch entry["event"] {
		case "http.request.started":
			if entry["request_id"] == expectedRequestID {
				seenStarted = true
			}
		case "http.request.completed":
			if entry["request_id"] == expectedRequestID && entry["status"] == float64(http.StatusOK) {
				seenCompleted = true
			}
		case "client.browser.window_error":
			seenClient = entry["related_request_id"] == "req-0123456789ab" && entry["related_action_id"] == "action-0123456789ab"
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !seenStarted || !seenCompleted || !seenClient {
		t.Fatalf("missing events: started=%v completed=%v client=%v\n%s", seenStarted, seenCompleted, seenClient, all.String())
	}
	for _, canary := range []string{secret, "page-secret-canary", "details-secret-canary"} {
		if strings.Contains(all.String(), canary) {
			t.Fatalf("client log leaked canary %q: %s", canary, all.String())
		}
	}
}

func TestCorrelationRemainsWhenRequestLifecycleLoggingIsDisabled(t *testing.T) {
	directory := t.TempDir()
	logging, err := observability.Start(observability.Options{
		Directory: &directory, Level: "info", MaxSizeMB: 1, MaxBackups: 1, Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.LogRESTRequests = false
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	payload := []byte(`{"level":"error","event":"browser.window_error","message":"failure"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/api/client-log", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "correlation-without-lifecycle")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	correlatedRequestID := response.Header.Get("X-Request-ID")
	testServer.Close()
	if err := logging.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Join(directory, observability.LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seenClient := false
	seenLifecycle := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry map[string]any
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry["event"] == "client.browser.window_error" && entry["request_id"] == correlatedRequestID && entry["action_id"] != "" {
			seenClient = true
		}
		if entry["event"] == "http.request.started" || entry["event"] == "http.request.completed" {
			seenLifecycle = true
		}
	}
	if !seenClient || seenLifecycle {
		t.Fatalf("correlation/lifecycle mismatch: client=%v lifecycle=%v", seenClient, seenLifecycle)
	}
}
