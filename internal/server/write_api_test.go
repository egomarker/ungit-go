package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egomarker/ungit-go/internal/config"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func makeMutationRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.name", "Test ungit")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

func startMutationServer(t *testing.T, configure func(*config.Config)) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.LaunchBrowser = false
	cfg.AutoFetch = false
	if configure != nil {
		configure(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func apiJSON(t *testing.T, ts *httptest.Server, method, path string, q url.Values, body any, target any) (int, []byte) {
	t.Helper()
	u := ts.URL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, u, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if target != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatalf("%s %s status=%d body=%q: %v", method, path, resp.StatusCode, data, err)
		}
	}
	return resp.StatusCode, data
}

func requireAPI200(t *testing.T, ts *httptest.Server, method, path string, q url.Values, body any) []byte {
	t.Helper()
	code, data := apiJSON(t, ts, method, path, q, body, nil)
	if code != http.StatusOK {
		t.Fatalf("%s %s: status=%d body=%s", method, path, code, data)
	}
	return data
}

func TestM3CommitPartialAndBasicWorkingTreeMutations(t *testing.T) {
	dir := makeMutationRepo(t)
	ts := startMutationServer(t, nil)

	// Partial commit: use two separated hunks, select the first changed pair,
	// and leave the second change in the working tree.
	baseLines := make([]string, 20)
	for i := range baseLines {
		baseLines[i] = fmt.Sprintf("line-%02d", i+1)
	}
	partialPath := filepath.Join(dir, "partial.txt")
	if err := os.WriteFile(partialPath, []byte(strings.Join(baseLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "partial.txt")
	gitRun(t, dir, "commit", "-q", "-m", "partial base")
	changedLines := append([]string(nil), baseLines...)
	changedLines[1] = "LINE-02"
	changedLines[17] = "LINE-18"
	if err := os.WriteFile(partialPath, []byte(strings.Join(changedLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	message := "partial"
	requireAPI200(t, ts, http.MethodPost, "/api/commit", nil, map[string]any{
		"path": dir, "message": message, "amend": false, "emptyCommit": false,
		"files": []map[string]any{{"name": "partial.txt", "patchLineList": []bool{true, true, false, false}}},
	})
	wantHead := append([]string(nil), baseLines...)
	wantHead[1] = "LINE-02"
	if got := gitRun(t, dir, "show", "HEAD:partial.txt"); got != strings.Join(wantHead, "\n") {
		t.Fatalf("partial commit content = %q", got)
	}
	if got := strings.TrimSpace(string(mustRead(t, partialPath))); got != strings.Join(changedLines, "\n") {
		t.Fatalf("working tree content = %q", got)
	}

	// Discard the remaining unstaged change.
	requireAPI200(t, ts, http.MethodPost, "/api/discardchanges", nil, map[string]any{"path": dir, "file": "partial.txt"})
	if got := gitRun(t, dir, "status", "--porcelain"); got != "" {
		t.Fatalf("status after discard = %q", got)
	}

	// Whole-file commit including an untracked file.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	message = "add new"
	requireAPI200(t, ts, http.MethodPost, "/api/commit", nil, map[string]any{
		"path": dir, "message": message, "files": []map[string]any{{"name": "new.txt", "patchLineList": nil}},
	})
	if got := gitRun(t, dir, "log", "-1", "--pretty=%s"); got != "add new" {
		t.Fatalf("latest message = %q", got)
	}

	// Ignore appends exactly one platform line ending plus the requested path.
	requireAPI200(t, ts, http.MethodPost, "/api/ignorefile", nil, map[string]any{"path": dir, "file": "tmp/"})
	ignore := string(mustRead(t, filepath.Join(dir, ".gitignore")))
	if !strings.HasSuffix(ignore, "tmp/") {
		t.Fatalf("gitignore = %q", ignore)
	}

	data := "dist/\n*.tmp\n"
	requireAPI200(t, ts, http.MethodPut, "/api/gitignore", nil, map[string]any{"path": dir, "data": data})
	if got := string(mustRead(t, filepath.Join(dir, ".gitignore"))); got != data {
		t.Fatalf("gitignore PUT = %q", got)
	}

	// Discard-all removes tracked modifications and untracked files.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("junk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireAPI200(t, ts, http.MethodPost, "/api/discardchanges", nil, map[string]any{"path": dir, "all": true})
	if got := gitRun(t, dir, "status", "--porcelain"); got != "" {
		t.Fatalf("status after discard-all = %q", got)
	}
}

func TestM3BranchesTagsCheckoutStashAndReset(t *testing.T) {
	dir := makeMutationRepo(t)
	ts := startMutationServer(t, nil)
	mainBranch := gitRun(t, dir, "branch", "--show-current")
	base := gitRun(t, dir, "rev-parse", "HEAD")

	requireAPI200(t, ts, http.MethodPost, "/api/branches", nil, map[string]any{"path": dir, "name": "feature", "sha1": base})
	if got := gitRun(t, dir, "show-ref", "--verify", "refs/heads/feature"); got == "" {
		t.Fatal("feature branch missing")
	}
	requireAPI200(t, ts, http.MethodPost, "/api/checkout", nil, map[string]any{"path": dir, "name": "feature"})
	if got := gitRun(t, dir, "branch", "--show-current"); got != "feature" {
		t.Fatalf("branch=%q", got)
	}

	requireAPI200(t, ts, http.MethodPost, "/api/tags", nil, map[string]any{"path": dir, "name": "v-m3", "sha1": "HEAD"})
	if got := gitRun(t, dir, "tag", "-l", "v-m3"); got != "v-m3" {
		t.Fatalf("tag=%q", got)
	}
	requireAPI200(t, ts, http.MethodDelete, "/api/tags", url.Values{"path": {dir}, "name": {"v-m3"}}, nil)
	if got := gitRun(t, dir, "tag", "-l", "v-m3"); got != "" {
		t.Fatalf("tag not deleted: %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "stash.txt"), []byte("stash me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireAPI200(t, ts, http.MethodPost, "/api/stashes", nil, map[string]any{"path": dir, "message": "m3 stash"})
	if got := gitRun(t, dir, "stash", "list"); !strings.Contains(got, "m3 stash") {
		t.Fatalf("stash list=%q", got)
	}
	requireAPI200(t, ts, http.MethodDelete, "/api/stashes/0", url.Values{"path": {dir}, "apply": {"true"}}, nil)
	if _, err := os.Stat(filepath.Join(dir, "stash.txt")); err != nil {
		t.Fatalf("stash apply did not restore untracked file: %v", err)
	}
	// apply leaves the stash; drop it separately.
	requireAPI200(t, ts, http.MethodDelete, "/api/stashes/0", url.Values{"path": {dir}}, nil)
	if got := gitRun(t, dir, "stash", "list"); got != "" {
		t.Fatalf("stash not dropped=%q", got)
	}
	_ = os.Remove(filepath.Join(dir, "stash.txt"))

	// Reset through the auto-stash wrapper; clean working tree makes it a direct reset.
	if err := os.WriteFile(filepath.Join(dir, "reset.txt"), []byte("reset\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "reset.txt")
	gitRun(t, dir, "commit", "-q", "-m", "reset target")
	requireAPI200(t, ts, http.MethodPost, "/api/reset", nil, map[string]any{"path": dir, "mode": "hard", "to": base})
	if got := gitRun(t, dir, "rev-parse", "HEAD"); got != base {
		t.Fatalf("reset HEAD=%q base=%q", got, base)
	}

	requireAPI200(t, ts, http.MethodPost, "/api/checkout", nil, map[string]any{"path": dir, "name": mainBranch})
	requireAPI200(t, ts, http.MethodDelete, "/api/branches", url.Values{"path": {dir}, "name": {"feature"}}, nil)
	if got := gitRun(t, dir, "branch", "--list", "feature"); got != "" {
		t.Fatalf("feature not deleted=%q", got)
	}
}

func TestM3RemotePushDeleteAndClone(t *testing.T) {
	dir := makeMutationRepo(t)
	ts := startMutationServer(t, nil)
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, filepath.Dir(remote), "init", "--bare", "-q", filepath.Base(remote))

	requireAPI200(t, ts, http.MethodPost, "/api/remotes/origin", nil, map[string]any{"path": dir, "url": remote})
	requireAPI200(t, ts, http.MethodPost, "/api/push", nil, map[string]any{"path": dir, "remote": "origin", "remoteBranch": "m3-main", "socketId": "ignore"})
	if got := gitRun(t, remote, "show-ref", "--verify", "refs/heads/m3-main"); got == "" {
		t.Fatal("remote branch was not pushed")
	}
	requireAPI200(t, ts, http.MethodDelete, "/api/remote/branches", url.Values{"path": {dir}, "remote": {"origin"}, "name": {"m3-main"}, "socketId": {"ignore"}}, nil)
	if out, err := exec.Command("git", "--git-dir="+remote, "show-ref", "--verify", "refs/heads/m3-main").CombinedOutput(); err == nil {
		t.Fatalf("remote branch still exists out=%s", out)
	}

	requireAPI200(t, ts, http.MethodPost, "/api/tags", nil, map[string]any{"path": dir, "name": "remote-tag"})
	requireAPI200(t, ts, http.MethodPost, "/api/push", nil, map[string]any{"path": dir, "remote": "origin", "refSpec": "refs/tags/remote-tag", "remoteBranch": "refs/tags/remote-tag", "socketId": "ignore"})
	requireAPI200(t, ts, http.MethodDelete, "/api/remote/tags", url.Values{"path": {dir}, "remote": {"origin"}, "name": {"remote-tag"}, "socketId": {"ignore"}}, nil)
	if out, err := exec.Command("git", "--git-dir="+remote, "show-ref", "--verify", "refs/tags/remote-tag").CombinedOutput(); err == nil {
		t.Fatalf("remote tag still exists out=%s", out)
	}

	// Re-push a branch so the bare repository has a cloneable HEAD target.
	branch := gitRun(t, dir, "branch", "--show-current")
	gitRun(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	requireAPI200(t, ts, http.MethodPost, "/api/push", nil, map[string]any{"path": dir, "remote": "origin", "remoteBranch": branch, "socketId": "ignore"})
	parent := t.TempDir()
	var cloneResp map[string]string
	code, data := apiJSON(t, ts, http.MethodPost, "/api/clone", nil, map[string]any{"path": parent, "url": remote, "destinationDir": "cloned", "socketId": "ignore"}, &cloneResp)
	if code != 200 {
		t.Fatalf("clone status=%d body=%s", code, data)
	}
	if got := gitRun(t, cloneResp["path"], "log", "-1", "--pretty=%s"); got != "initial" {
		t.Fatalf("cloned log=%q", got)
	}

	requireAPI200(t, ts, http.MethodDelete, "/api/remotes/origin", url.Values{"path": {dir}}, nil)
	if got := gitRun(t, dir, "remote"); got != "" {
		t.Fatalf("remote not removed=%q", got)
	}
}

func TestM3HistoryMutations(t *testing.T) {
	dir := makeMutationRepo(t)
	ts := startMutationServer(t, func(c *config.Config) { c.NoFFMerge = false })
	main := gitRun(t, dir, "branch", "--show-current")
	base := gitRun(t, dir, "rev-parse", "HEAD")

	// Commit on feature, then cherry-pick it onto main.
	gitRun(t, dir, "checkout", "-q", "-b", "cp")
	if err := os.WriteFile(filepath.Join(dir, "cp.txt"), []byte("cp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "cp.txt")
	gitRun(t, dir, "commit", "-q", "-m", "cp commit")
	cpSHA := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", main)
	requireAPI200(t, ts, http.MethodPost, "/api/cherrypick", nil, map[string]any{"path": dir, "name": cpSHA})
	if got := gitRun(t, dir, "log", "-1", "--pretty=%s"); got != "cp commit" {
		t.Fatalf("cherry-pick message=%q", got)
	}

	// Revert the cherry-picked commit.
	revertTarget := gitRun(t, dir, "rev-parse", "HEAD")
	requireAPI200(t, ts, http.MethodPost, "/api/revert", nil, map[string]any{"path": dir, "commit": revertTarget})
	if _, err := os.Stat(filepath.Join(dir, "cp.txt")); !os.IsNotExist(err) {
		t.Fatalf("revert did not remove cp.txt, err=%v", err)
	}

	// Merge a simple branch.
	gitRun(t, dir, "checkout", "-q", "-b", "merge-source")
	if err := os.WriteFile(filepath.Join(dir, "merge.txt"), []byte("merge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "merge.txt")
	gitRun(t, dir, "commit", "-q", "-m", "merge source")
	gitRun(t, dir, "checkout", "-q", main)
	requireAPI200(t, ts, http.MethodPost, "/api/merge", nil, map[string]any{"path": dir, "with": "merge-source"})
	if _, err := os.Stat(filepath.Join(dir, "merge.txt")); err != nil {
		t.Fatalf("merge did not add file: %v", err)
	}

	// Squash another branch leaves its change staged but does not commit it.
	gitRun(t, dir, "checkout", "-q", "-b", "squash-source")
	if err := os.WriteFile(filepath.Join(dir, "squash.txt"), []byte("squash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "squash.txt")
	gitRun(t, dir, "commit", "-q", "-m", "squash source")
	gitRun(t, dir, "checkout", "-q", main)
	requireAPI200(t, ts, http.MethodPost, "/api/squash", nil, map[string]any{"path": dir, "target": "squash-source"})
	if got := gitRun(t, dir, "diff", "--cached", "--name-only"); got != "squash.txt" {
		t.Fatalf("squash staged=%q", got)
	}
	gitRun(t, dir, "reset", "--hard", "-q", "HEAD")

	// Rebase a non-conflicting branch onto current main.
	gitRun(t, dir, "branch", "rebase-source", base)
	gitRun(t, dir, "checkout", "-q", "rebase-source")
	if err := os.WriteFile(filepath.Join(dir, "rebase.txt"), []byte("rebase\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "rebase.txt")
	gitRun(t, dir, "commit", "-q", "-m", "rebase source")
	requireAPI200(t, ts, http.MethodPost, "/api/rebase", nil, map[string]any{"path": dir, "onto": main})
	if got := gitRun(t, dir, "merge-base", "HEAD", main); got != gitRun(t, dir, "rev-parse", main) {
		t.Fatalf("rebase merge-base=%q", got)
	}
}

func TestM3MergeConflictResolveContinueAndAbort(t *testing.T) {
	dir := makeMutationRepo(t)
	ts := startMutationServer(t, func(c *config.Config) { c.NoFFMerge = false })
	main := gitRun(t, dir, "branch", "--show-current")
	base := gitRun(t, dir, "rev-parse", "HEAD")

	gitRun(t, dir, "checkout", "-q", "-b", "conflict", base)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "feature conflict")
	gitRun(t, dir, "checkout", "-q", main)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "main conflict")

	code, _ := apiJSON(t, ts, http.MethodPost, "/api/merge", nil, map[string]any{"path": dir, "with": "conflict"}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("conflicting merge status=%d, want 400", code)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireAPI200(t, ts, http.MethodPost, "/api/resolveconflicts", nil, map[string]any{"path": dir, "files": []string{"a.txt"}})
	requireAPI200(t, ts, http.MethodPost, "/api/merge/continue", nil, map[string]any{"path": dir, "message": "resolved merge"})
	if got := gitRun(t, dir, "log", "-1", "--pretty=%s"); got != "resolved merge" {
		t.Fatalf("merge continuation message=%q", got)
	}

	// Create another conflicting branch from the new main and exercise abort.
	newBase := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "-b", "abort-source", newBase)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("abort source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "abort source")
	gitRun(t, dir, "checkout", "-q", main)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("abort main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "abort main")
	code, _ = apiJSON(t, ts, http.MethodPost, "/api/merge", nil, map[string]any{"path": dir, "with": "abort-source"}, nil)
	if code != 400 {
		t.Fatalf("abort merge setup status=%d", code)
	}
	requireAPI200(t, ts, http.MethodPost, "/api/merge/abort", nil, map[string]any{"path": dir})
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("merge state remains after abort: %v", err)
	}
}

func TestM3InitCreateDirAndSubmoduleLifecycle(t *testing.T) {
	ts := startMutationServer(t, nil)
	parent := t.TempDir()
	newDir := filepath.Join(parent, "created", "nested")
	requireAPI200(t, ts, http.MethodPost, "/api/createdir", nil, map[string]any{"dir": newDir})
	requireAPI200(t, ts, http.MethodPost, "/api/init", nil, map[string]any{"path": newDir})
	if _, err := os.Stat(filepath.Join(newDir, ".git")); err != nil {
		t.Fatalf("init did not create .git: %v", err)
	}

	// Submodule operations use the normal Git transport. Allow file transport in
	// this isolated test so no network service is required.
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	source := makeMutationRepo(t)
	parentRepo := makeMutationRepo(t)
	requireAPI200(t, ts, http.MethodPost, "/api/submodules/add", nil, map[string]any{"path": parentRepo, "submoduleUrl": source, "submodulePath": "vendor/sub"})
	if _, err := os.Stat(filepath.Join(parentRepo, "vendor", "sub", ".git")); err != nil {
		t.Fatalf("submodule add failed: %v", err)
	}
	requireAPI200(t, ts, http.MethodPost, "/api/submodules/update", nil, map[string]any{"path": parentRepo})
	requireAPI200(t, ts, http.MethodDelete, "/api/submodules", url.Values{"path": {parentRepo}, "submoduleName": {"vendor/sub"}, "submodulePath": {"vendor/sub"}}, nil)
	if _, err := os.Stat(filepath.Join(parentRepo, "vendor", "sub")); !os.IsNotExist(err) {
		t.Fatalf("submodule path remains: %v", err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestM3WriteRoutesRejectMissingPath(t *testing.T) {
	ts := startMutationServer(t, nil)
	missing := filepath.Join(t.TempDir(), "missing")
	code, data := apiJSON(t, ts, http.MethodPost, "/api/commit", nil, map[string]any{"path": missing, "message": "x", "files": []any{}}, nil)
	if code != 400 || !strings.Contains(string(data), "no-such-path") {
		t.Fatalf("missing path status=%d body=%s", code, data)
	}
}

func Example_m3MutationSurface() {
	fmt.Println("init clone push reset discard commit revert branches tags checkout cherry-pick remotes merge squash rebase conflicts submodules stashes gitignore")
	// Output: init clone push reset discard commit revert branches tags checkout cherry-pick remotes merge squash rebase conflicts submodules stashes gitignore
}

func TestM3CommitAmendEmptyAndRemoval(t *testing.T) {
	dir := makeMutationRepo(t)
	ts := startMutationServer(t, nil)

	// Empty commits are explicitly supported by the UI.
	requireAPI200(t, ts, http.MethodPost, "/api/commit", nil, map[string]any{
		"path": dir, "message": "empty", "files": []any{}, "emptyCommit": true,
	})
	if got := gitRun(t, dir, "log", "-1", "--pretty=%s"); got != "empty" {
		t.Fatalf("empty commit message=%q", got)
	}
	count := gitRun(t, dir, "rev-list", "--count", "HEAD")

	// Amend with no files is allowed and should not add a commit.
	requireAPI200(t, ts, http.MethodPost, "/api/commit", nil, map[string]any{
		"path": dir, "message": "amended empty", "files": []any{}, "amend": true,
	})
	if got := gitRun(t, dir, "rev-list", "--count", "HEAD"); got != count {
		t.Fatalf("amend changed commit count: before=%s after=%s", count, got)
	}
	if got := gitRun(t, dir, "log", "-1", "--pretty=%s"); got != "amended empty" {
		t.Fatalf("amend message=%q", got)
	}

	// Removed files are staged with update-index --remove --stdin, mirroring Node.
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	requireAPI200(t, ts, http.MethodPost, "/api/commit", nil, map[string]any{
		"path": dir, "message": "remove a", "files": []map[string]any{{"name": "a.txt", "patchLineList": nil}},
	})
	if out, err := exec.Command("git", "-C", dir, "cat-file", "-e", "HEAD:a.txt").CombinedOutput(); err == nil {
		t.Fatalf("removed file remains in HEAD: %s", out)
	}
}

func TestM3RebaseConflictContinueAndAbort(t *testing.T) {
	setup := func(t *testing.T) (dir, main, source string) {
		t.Helper()
		dir = makeMutationRepo(t)
		main = gitRun(t, dir, "branch", "--show-current")
		base := gitRun(t, dir, "rev-parse", "HEAD")
		gitRun(t, dir, "checkout", "-q", "-b", "rb-source", base)
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("source\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "a.txt")
		gitRun(t, dir, "commit", "-q", "-m", "rb source")
		source = gitRun(t, dir, "branch", "--show-current")
		gitRun(t, dir, "checkout", "-q", main)
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "a.txt")
		gitRun(t, dir, "commit", "-q", "-m", "rb main")
		gitRun(t, dir, "checkout", "-q", source)
		return
	}

	t.Run("continue", func(t *testing.T) {
		dir, main, _ := setup(t)
		ts := startMutationServer(t, nil)
		code, _ := apiJSON(t, ts, http.MethodPost, "/api/rebase", nil, map[string]any{"path": dir, "onto": main}, nil)
		if code != 400 {
			t.Fatalf("conflicting rebase status=%d", code)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("rebased resolved\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		requireAPI200(t, ts, http.MethodPost, "/api/resolveconflicts", nil, map[string]any{"path": dir, "files": []string{"a.txt"}})
		requireAPI200(t, ts, http.MethodPost, "/api/rebase/continue", nil, map[string]any{"path": dir})
		if _, err := os.Stat(filepath.Join(dir, ".git", "rebase-merge")); !os.IsNotExist(err) {
			if _, err2 := os.Stat(filepath.Join(dir, ".git", "rebase-apply")); !os.IsNotExist(err2) {
				t.Fatalf("rebase state remains: merge=%v apply=%v", err, err2)
			}
		}
	})

	t.Run("abort", func(t *testing.T) {
		dir, main, _ := setup(t)
		ts := startMutationServer(t, nil)
		before := gitRun(t, dir, "rev-parse", "HEAD")
		code, _ := apiJSON(t, ts, http.MethodPost, "/api/rebase", nil, map[string]any{"path": dir, "onto": main}, nil)
		if code != 400 {
			t.Fatalf("conflicting rebase status=%d", code)
		}
		requireAPI200(t, ts, http.MethodPost, "/api/rebase/abort", nil, map[string]any{"path": dir})
		if got := gitRun(t, dir, "rev-parse", "HEAD"); got != before {
			t.Fatalf("rebase abort HEAD=%s before=%s", got, before)
		}
	})
}

func TestM3BareInit(t *testing.T) {
	ts := startMutationServer(t, nil)
	dir := t.TempDir()
	requireAPI200(t, ts, http.MethodPost, "/api/init", nil, map[string]any{"path": dir, "bare": true})
	if got := gitRun(t, dir, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Fatalf("bare init result=%q", got)
	}
}
