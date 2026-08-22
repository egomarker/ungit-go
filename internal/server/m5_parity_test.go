package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egomarker/ungit-go/internal/config"
)

func TestM5NodeRouteInventory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Default()
	cfg.Dev = true
	cfg.AutoFetch = false
	cfg.LogRESTRequests = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	oldLatest := latestVersionLookup
	latestVersionLookup = func(context.Context) (string, error) { return s.version, nil }
	defer func() { latestVersionLookup = oldLatest }()

	type route struct{ method, path string }
	routes := []route{
		{"GET", "/"}, {"GET", "/serverdata.js"}, {"GET", "/api/latestversion"}, {"GET", "/api/ping"}, {"GET", "/api/gitversion"},
		{"GET", "/api/userconfig"}, {"POST", "/api/userconfig"}, {"GET", "/api/fs/exists?path=/definitely/missing"}, {"GET", "/api/fs/listDirectories?term=/definitely/missing"},
		{"GET", "/api/status?path=/definitely/missing"}, {"POST", "/api/init"}, {"POST", "/api/clone"}, {"GET", "/api/fetch?path=/definitely/missing"}, {"POST", "/api/push"}, {"POST", "/api/reset"},
		{"GET", "/api/diff?path=/definitely/missing"}, {"GET", "/api/diff/image?path=/definitely/missing"}, {"POST", "/api/discardchanges"}, {"POST", "/api/ignorefile"}, {"POST", "/api/commit"}, {"POST", "/api/revert"},
		{"GET", "/api/gitlog?path=/definitely/missing"}, {"GET", "/api/show?path=/definitely/missing"}, {"GET", "/api/head?path=/definitely/missing"}, {"GET", "/api/refs?path=/definitely/missing"}, {"GET", "/api/branches?path=/definitely/missing"},
		{"POST", "/api/branches"}, {"DELETE", "/api/branches?path=/definitely/missing"}, {"DELETE", "/api/remote/branches?path=/definitely/missing"}, {"GET", "/api/tags?path=/definitely/missing"}, {"GET", "/api/remote/tags?path=/definitely/missing"},
		{"POST", "/api/tags"}, {"DELETE", "/api/tags?path=/definitely/missing"}, {"DELETE", "/api/remote/tags?path=/definitely/missing"}, {"POST", "/api/checkout"}, {"POST", "/api/cherrypick"}, {"GET", "/api/checkout?path=/definitely/missing"},
		{"GET", "/api/remotes?path=/definitely/missing"}, {"GET", "/api/remotes/origin?path=/definitely/missing"}, {"POST", "/api/remotes/origin"}, {"DELETE", "/api/remotes/origin?path=/definitely/missing"},
		{"POST", "/api/merge"}, {"POST", "/api/merge/continue"}, {"POST", "/api/merge/abort"}, {"POST", "/api/squash"}, {"POST", "/api/rebase"}, {"POST", "/api/rebase/continue"}, {"POST", "/api/rebase/abort"}, {"POST", "/api/resolveconflicts"},
		{"POST", "/api/launchmergetool"}, {"GET", "/api/baserepopath?path=/definitely/missing"}, {"GET", "/api/submodules?path=/definitely/missing"}, {"POST", "/api/submodules/update"}, {"POST", "/api/submodules/add"}, {"DELETE", "/api/submodules?path=/definitely/missing"},
		{"GET", "/api/quickstatus?path=/definitely/missing"}, {"GET", "/api/stashes?path=/definitely/missing"}, {"POST", "/api/stashes"}, {"DELETE", "/api/stashes/0?path=/definitely/missing"}, {"GET", "/api/gitconfig"}, {"GET", "/api/credentials?socketId=missing&remote=x"},
		{"POST", "/api/createdir"}, {"GET", "/api/gitignore?path=/definitely/missing"}, {"PUT", "/api/gitignore"},
		{"POST", "/api/testing/createtempdir"}, {"POST", "/api/testing/createfile"}, {"POST", "/api/testing/changefile"}, {"POST", "/api/testing/createimagefile"}, {"POST", "/api/testing/changeimagefile"}, {"POST", "/api/testing/removefile"}, {"POST", "/api/testing/git"}, {"POST", "/api/testing/cleanup"},
	}
	client := ts.Client()
	for _, rt := range routes {
		t.Run(rt.method+" "+strings.Split(rt.path, "?")[0], func(t *testing.T) {
			var body io.Reader
			if rt.method == http.MethodPost || rt.method == http.MethodPut {
				body = bytes.NewBufferString(`{}`)
			}
			req, err := http.NewRequest(rt.method, ts.URL+rt.path, body)
			if err != nil {
				t.Fatal(err)
			}
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
				t.Fatalf("route missing: status=%d", resp.StatusCode)
			}
		})
	}
}

func TestM5DevTestingAPI(t *testing.T) {
	cfg := config.Default()
	cfg.Dev = true
	cfg.LogRESTRequests = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/testing/createtempdir", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if created["path"] == "" {
		t.Fatal("empty temp path")
	}
	file := filepath.Join(created["path"], "a.txt")
	payload, _ := json.Marshal(map[string]string{"path": created["path"], "file": file, "content": "hello"})
	resp, err = http.Post(ts.URL+"/api/testing/createfile", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("createfile status=%d", resp.StatusCode)
	}
	resp, err = http.Post(ts.URL+"/api/testing/cleanup", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("cleanup status=%d", resp.StatusCode)
	}
}

func TestM5LatestVersionMajorMinorParity(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.6.0", "1.5.30", true},
		{"2.0.0", "1.9.99", true},
		{"1.5.99", "1.5.1", false},
		{"1.5.30", "1.5.30-piclaw.1", false},
		{"2.0.0", "dev-1.5.30-abc", false},
	}
	for _, tc := range cases {
		if got := majorMinorGreater(tc.latest, tc.current); got != tc.want {
			t.Fatalf("majorMinorGreater(%q,%q)=%v want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestM5RealtimeClientsReleased(t *testing.T) {
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

	for i := 0; i < 20; i++ {
		resp, _, socketID, cancel := connectRealtime(t, ts)
		realtimeEmit(t, ts, socketID, "watch", map[string]string{"path": dir})
		cancel()
		_ = resp.Body.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.realtime.mu.Lock()
		n := len(s.realtime.clients)
		s.realtime.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.realtime.mu.Lock()
	n := len(s.realtime.clients)
	s.realtime.mu.Unlock()
	t.Fatalf("realtime clients leaked: %d remain", n)
}

func TestM5ConcurrentReadLoad(t *testing.T) {
	dir := makeMutationRepo(t)
	cfg := config.Default()
	cfg.AutoFetch = false
	cfg.LogRESTRequests = false
	cfg.MaxConcurrentGitOperations = 4
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := ts.Client().Get(ts.URL + "/api/status?path=" + url.QueryEscape(dir))
			if err != nil {
				errCh <- err
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 {
				errCh <- &statusError{code: resp.StatusCode}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

type statusError struct{ code int }

func (e *statusError) Error() string { return http.StatusText(e.code) }
