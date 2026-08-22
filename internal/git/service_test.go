package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/egomarker/ungit-go/internal/config"
)

func testRepo(t *testing.T) string {
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
	return dir
}

func TestServiceStatusLogAndRevParse(t *testing.T) {
	dir := testRepo(t)
	cfg := config.Default()
	cfg.LaunchBrowser = false
	s := NewService(cfg)
	ctx := context.Background()
	rp := s.RevParse(ctx, dir)
	if rp.Type != "inited" || filepath.Clean(rp.GitRootPath) != filepath.Clean(dir) {
		t.Fatalf("revparse=%#v", rp)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := st.Files["a.txt"]
	if !ok || !f.IsNew || f.Additions != "-" {
		t.Fatalf("status=%#v", st)
	}
	cmd := exec.Command("git", "add", "a.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add %v %s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "first")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit %v %s", err, out)
	}
	logRes, err := s.Log(ctx, dir, 25, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(logRes.Nodes) != 1 || logRes.Nodes[0].Message != "first" {
		t.Fatalf("log=%#v", logRes)
	}
	head, err := s.Head(ctx, dir)
	if err != nil || len(head) != 1 || head[0].Message != "first" {
		t.Fatalf("head=%#v err=%v", head, err)
	}
}

func TestParseIntMatchesJavaScriptParseInt(t *testing.T) {
	cases := []struct {
		value    string
		fallback int
		want     int
		wantErr  bool
	}{
		{"", 25, 25, false},
		{"0", 25, 0, false},
		{" 12px", 25, 12, false},
		{"-3rest", 25, -3, false},
		{"abc", 25, 0, true},
	}
	for _, tc := range cases {
		got, err := ParseInt(tc.value, tc.fallback)
		if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
			t.Fatalf("ParseInt(%q,%d)=(%d,%v), want (%d,err=%v)", tc.value, tc.fallback, got, err, tc.want, tc.wantErr)
		}
	}
}
