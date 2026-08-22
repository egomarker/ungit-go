package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/egomarker/ungit-go/internal/config"
)

func makeReadRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.name", "Test ungit")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "first")
	run("tag", "v1")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func startTestHTTPServer(h http.Handler) *httptest.Server { return httptest.NewServer(h) }

func getJSON(t *testing.T, base, path string, q url.Values, target any) int {
	t.Helper()
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if target != nil {
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, target); err != nil {
			t.Fatalf("%s status=%d body=%q: %v", path, resp.StatusCode, b, err)
		}
	}
	return resp.StatusCode
}

func TestM2ReadEndpoints(t *testing.T) {
	dir := makeReadRepo(t)
	cfg := config.Default()
	cfg.LaunchBrowser = false
	cfg.AutoFetch = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := startTestHTTPServer(s.Handler())
	defer ts.Close()
	q := func(extra ...string) url.Values {
		v := url.Values{"path": {dir}}
		for i := 0; i+1 < len(extra); i += 2 {
			v.Set(extra[i], extra[i+1])
		}
		return v
	}

	var quick map[string]any
	if code := getJSON(t, ts.URL, "/api/quickstatus", q(), &quick); code != 200 || quick["type"] != "inited" {
		t.Fatalf("quick code=%d body=%#v", code, quick)
	}
	var status map[string]any
	if code := getJSON(t, ts.URL, "/api/status", q(), &status); code != 200 {
		t.Fatalf("status=%d %#v", code, status)
	}
	files := status["files"].(map[string]any)
	if _, ok := files["a.txt"]; !ok {
		t.Fatalf("status files=%#v", files)
	}
	var log map[string]any
	getJSON(t, ts.URL, "/api/gitlog", q("limit", "25", "skip", "0"), &log)
	nodes := log["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["message"] != "first" {
		t.Fatalf("log=%#v", log)
	}
	var head []any
	getJSON(t, ts.URL, "/api/head", q(), &head)
	if len(head) != 1 {
		t.Fatalf("head=%#v", head)
	}
	var refs []map[string]string
	getJSON(t, ts.URL, "/api/refs", q(), &refs)
	if len(refs) < 2 {
		t.Fatalf("refs=%#v", refs)
	}
	var branches []map[string]any
	getJSON(t, ts.URL, "/api/branches", q(), &branches)
	if len(branches) != 1 || branches[0]["current"] != true {
		t.Fatalf("branches=%#v", branches)
	}
	var tags []string
	getJSON(t, ts.URL, "/api/tags", q(), &tags)
	if len(tags) != 1 || tags[0] != "v1" {
		t.Fatalf("tags=%#v", tags)
	}
	var checkout string
	getJSON(t, ts.URL, "/api/checkout", q(), &checkout)
	if checkout == "" {
		t.Fatal("empty current branch")
	}
	var remotes []any
	getJSON(t, ts.URL, "/api/remotes", q(), &remotes)
	if len(remotes) != 0 {
		t.Fatalf("remotes=%#v", remotes)
	}
	var diff string
	getJSON(t, ts.URL, "/api/diff", q("file", "a.txt"), &diff)
	if diff == "" {
		t.Fatal("empty diff")
	}
	var ignore map[string]string
	getJSON(t, ts.URL, "/api/gitignore", q(), &ignore)
	if ignore["content"] != "ignored/\n" {
		t.Fatalf("ignore=%#v", ignore)
	}
	var stashes []any
	getJSON(t, ts.URL, "/api/stashes", q(), &stashes)
	if len(stashes) != 0 {
		t.Fatalf("stashes=%#v", stashes)
	}
	var submodules any
	getJSON(t, ts.URL, "/api/submodules", q(), &submodules)
	if _, ok := submodules.(map[string]any); !ok {
		t.Fatalf("submodules=%#v", submodules)
	}
	var base map[string]any
	getJSON(t, ts.URL, "/api/baserepopath", q(), &base)
	if len(base) != 0 {
		t.Fatalf("base=%#v", base)
	}
	var exists bool
	getJSON(t, ts.URL, "/api/fs/exists", q(), &exists)
	if !exists {
		t.Fatal("fs exists=false")
	}
}
