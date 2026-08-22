package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egomarker/ungit-go/internal/config"
	gitinfo "github.com/egomarker/ungit-go/internal/git"
)

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	cfg := config.Default()
	cfg.LaunchBrowser = false
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestPing(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
}

func TestIndexContainsAllBuiltInComponents(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if strings.Contains(text, "<!-- ungit-plugins-placeholder -->") {
		t.Fatal("plugin placeholder was not replaced")
	}
	if got := strings.Count(text, "<!-- Component:"); got != 20 {
		t.Fatalf("component count = %d, want 20", got)
	}
	if !strings.Contains(text, "/plugins/app/app.bundle.js") {
		t.Fatal("app component bundle was not injected")
	}
}

func TestGitVersionEndpoint(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/api/gitversion")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got gitinfo.VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.RequiredVersion != gitinfo.RequiredVersion {
		t.Fatalf("requiredVersion = %q", got.RequiredVersion)
	}
	if got.Version == "" {
		t.Fatal("empty Git version")
	}
}

func TestSocketClientShimIsServed(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/socket.io/socket.io.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "global.io") || !strings.Contains(string(body), "connected") {
		t.Fatalf("unexpected socket shim: %q", body)
	}
}
