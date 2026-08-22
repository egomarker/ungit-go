package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egomarker/ungit-go/internal/config"
)

type sseTestEvent struct {
	name string
	data []byte
}

func readSSEEvent(t *testing.T, r *bufio.Reader) sseTestEvent {
	t.Helper()
	var ev sseTestEvent
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if ev.name != "" {
				return ev
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			ev.name = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			ev.data = append(ev.data, strings.TrimPrefix(line, "data: ")...)
		}
	}
}

func connectRealtime(t *testing.T, ts *httptest.Server) (*http.Response, *bufio.Reader, string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/realtime/connect", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	reader := bufio.NewReader(resp.Body)
	ev := readSSEEvent(t, reader)
	if ev.name != "connected" {
		cancel()
		t.Fatalf("first SSE event=%q", ev.name)
	}
	var payload map[string]string
	if err := json.Unmarshal(ev.data, &payload); err != nil {
		cancel()
		t.Fatal(err)
	}
	return resp, reader, payload["socketId"], cancel
}

func realtimeEmit(t *testing.T, ts *httptest.Server, socketID, event string, data any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"event": event, "data": data})
	resp, err := http.Post(ts.URL+"/realtime/emit?socketId="+url.QueryEscape(socketID), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("emit %s: status=%d body=%s", event, resp.StatusCode, b)
	}
}

func TestM4RealtimeWatchEmitsWorkingTreeAndGitChanges(t *testing.T) {
	dir := makeMutationRepo(t)
	cfg := config.Default()
	cfg.AutoFetch = false
	cfg.LogRESTRequests = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, reader, socketID, cancel := connectRealtime(t, ts)
	defer cancel()
	defer resp.Body.Close()
	realtimeEmit(t, ts, socketID, "watch", map[string]string{"path": dir})

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed from outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for {
		ev := readSSEEvent(t, reader)
		if ev.name == "working-tree-changed" {
			break
		}
	}

	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "external commit")
	for {
		ev := readSSEEvent(t, reader)
		if ev.name == "git-directory-changed" {
			break
		}
	}
}

func TestM4CredentialBrokerRoundTrip(t *testing.T) {
	cfg := config.Default()
	cfg.LogRESTRequests = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, reader, socketID, cancel := connectRealtime(t, ts)
	defer cancel()
	defer resp.Body.Close()

	result := make(chan []byte, 1)
	go func() {
		r, err := http.Get(ts.URL + "/api/credentials?socketId=" + url.QueryEscape(socketID) + "&remote=" + url.QueryEscape("https://example/repo"))
		if err != nil {
			result <- []byte("ERR:" + err.Error())
			return
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		result <- b
	}()

	ev := readSSEEvent(t, reader)
	if ev.name != "request-credentials" || !bytes.Contains(ev.data, []byte("https://example/repo")) {
		t.Fatalf("credential event=%q data=%s", ev.name, ev.data)
	}
	realtimeEmit(t, ts, socketID, "credentials", map[string]string{"username": "alice", "password": "secret"})
	var got credentialPayload
	if err := json.Unmarshal(<-result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || got.Password != "secret" {
		t.Fatalf("credentials=%#v", got)
	}
}

func TestM4AuthenticationContract(t *testing.T) {
	cfg := config.Default()
	cfg.Authentication = true
	cfg.Users = map[string]string{"alice": "secret"}
	cfg.LogRESTRequests = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	get := func(path string) (int, []byte) {
		r, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, b
	}
	if code, body := get("/api/status?path=/tmp"); code != 401 || !bytes.Contains(body, []byte("authentication-required")) {
		t.Fatalf("protected status=%d body=%s", code, body)
	}
	if code, body := get("/api/loggedin"); code != 200 || !bytes.Contains(body, []byte(`"loggedIn":false`)) {
		t.Fatalf("loggedin status=%d body=%s", code, body)
	}
	loginBody := bytes.NewBufferString(`{"username":"alice","password":"secret"}`)
	r, err := client.Post(ts.URL+"/api/login", "application/json", loginBody)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("login status=%d", r.StatusCode)
	}
	if code, body := get("/api/loggedin"); code != 200 || !bytes.Contains(body, []byte(`"loggedIn":true`)) {
		t.Fatalf("loggedin status=%d body=%s", code, body)
	}
	if code, body := get("/serverdata.js"); code != 200 || bytes.Contains(body, []byte("secret")) || !bytes.Contains(body, []byte(`"users":null`)) {
		t.Fatalf("serverdata status=%d body=%s", code, body)
	}
	if code, _ := get("/api/logout"); code != 200 {
		t.Fatalf("logout status=%d", code)
	}
	if code, body := get("/api/status?path=/tmp"); code != 401 || !bytes.Contains(body, []byte("authentication-required")) {
		t.Fatalf("post-logout protected status=%d body=%s", code, body)
	}
}

func TestM4RootPathRoutesRealtimeAndAPI(t *testing.T) {
	cfg := config.Default()
	cfg.RootPath = "/ungit"
	cfg.LogRESTRequests = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/ungit/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("root-path ping status=%d", resp.StatusCode)
	}
	resp, err = http.Get(ts.URL + "/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("outside root status=%d", resp.StatusCode)
	}
}
