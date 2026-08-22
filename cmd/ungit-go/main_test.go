package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egomarker/ungit-go/internal/config"
)

func TestBrowserURLDefaultsToCurrentWorkingDirectory(t *testing.T) {
	cfg := config.Default()
	cfg.ForcedLaunchPath = nil
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got := browserURL(cfg, 8448)
	if !strings.Contains(got, "#/repository?path=") || !strings.Contains(got, strings.ReplaceAll(cwd, " ", "%20")) {
		t.Fatalf("url=%q cwd=%q", got, cwd)
	}
}

func TestBrowserURLEmptyForcedPathOpensHome(t *testing.T) {
	cfg := config.Default()
	empty := ""
	cfg.ForcedLaunchPath = &empty
	got := browserURL(cfg, 8448)
	if strings.Contains(got, "#/repository") {
		t.Fatalf("url=%q", got)
	}
}

func TestEncodePathMatchesNodeEncodeURIComponentBehavior(t *testing.T) {
	cases := map[string]string{
		"/tmp/a b":   "/tmp/a%20b",
		`C:\foo bar`: `C%3A%5Cfoo%20bar`,
		"a#b?c":      "a%23b%3Fc",
		"-_.!~*'()":  "-_.!~*'()",
	}
	for in, want := range cases {
		if got := encodePathLikeNode(in); got != want {
			t.Fatalf("encodePathLikeNode(%q)=%q want %q", in, got, want)
		}
	}
}
