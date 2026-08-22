package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	gitapi "github.com/egomarker/ungit-go/internal/git"
)

func (s *Server) registerWriteAPI(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/init", s.postInit)
	mux.HandleFunc("POST /api/clone", s.postClone)
	mux.HandleFunc("POST /api/push", s.postPush)
	mux.HandleFunc("POST /api/reset", s.postReset)
	mux.HandleFunc("POST /api/discardchanges", s.postDiscardChanges)
	mux.HandleFunc("POST /api/ignorefile", s.postIgnoreFile)
	mux.HandleFunc("POST /api/commit", s.postCommit)
	mux.HandleFunc("POST /api/revert", s.postRevert)
	mux.HandleFunc("POST /api/branches", s.postBranch)
	mux.HandleFunc("DELETE /api/branches", s.deleteBranch)
	mux.HandleFunc("DELETE /api/remote/branches", s.deleteRemoteBranch)
	mux.HandleFunc("POST /api/tags", s.postTag)
	mux.HandleFunc("DELETE /api/tags", s.deleteTag)
	mux.HandleFunc("DELETE /api/remote/tags", s.deleteRemoteTag)
	mux.HandleFunc("POST /api/checkout", s.postCheckout)
	mux.HandleFunc("POST /api/cherrypick", s.postCherryPick)
	mux.HandleFunc("POST /api/remotes/{name}", s.postRemote)
	mux.HandleFunc("DELETE /api/remotes/{name}", s.deleteRemote)
	mux.HandleFunc("POST /api/merge", s.postMerge)
	mux.HandleFunc("POST /api/merge/continue", s.postMergeContinue)
	mux.HandleFunc("POST /api/merge/abort", s.postMergeAbort)
	mux.HandleFunc("POST /api/squash", s.postSquash)
	mux.HandleFunc("POST /api/rebase", s.postRebase)
	mux.HandleFunc("POST /api/rebase/continue", s.postRebaseContinue)
	mux.HandleFunc("POST /api/rebase/abort", s.postRebaseAbort)
	mux.HandleFunc("POST /api/resolveconflicts", s.postResolveConflicts)
	mux.HandleFunc("POST /api/launchmergetool", s.postLaunchMergeTool)
	mux.HandleFunc("POST /api/submodules/update", s.postSubmodulesUpdate)
	mux.HandleFunc("POST /api/submodules/add", s.postSubmodulesAdd)
	mux.HandleFunc("DELETE /api/submodules", s.deleteSubmodule)
	mux.HandleFunc("POST /api/stashes", s.postStash)
	mux.HandleFunc("DELETE /api/stashes/{id}", s.deleteStash)
	mux.HandleFunc("POST /api/createdir", s.postCreateDir)
	mux.HandleFunc("PUT /api/gitignore", s.putGitIgnore)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func ensureExistingPath(w http.ResponseWriter, p string) bool {
	if _, err := os.Stat(p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "'No such path: " + p, "errorCode": "no-such-path"})
		return false
	}
	return true
}

func runWriteResult(w http.ResponseWriter, value string, err error) {
	writeResult(w, value, err)
}

func (s *Server) postInit(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		Bare bool   `json:"bare"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	args := []string{"init"}
	if b.Bare {
		args = []string{"init", "--bare", "--shared"}
	}
	out, err := s.git.RunMutation(r.Context(), b.Path, args...)
	runWriteResult(w, out, err)
}

func (s *Server) postClone(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path                 string `json:"path"`
		URL                  string `json:"url"`
		DestinationDir       string `json:"destinationDir"`
		IsRecursiveSubmodule bool   `json:"isRecursiveSubmodule"`
		SocketID             any    `json:"socketId"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	if _, ok := s.requireSocket(w, b.SocketID); !ok {
		return
	}
	url := strings.TrimSpace(b.URL)
	if strings.HasPrefix(url, "git clone ") {
		url = strings.TrimPrefix(url, "git clone ")
	}
	dest := strings.TrimSpace(b.DestinationDir)
	args := []string{"clone", url, dest}
	if b.IsRecursiveSubmodule {
		args = append(args, "--recurse-submodules")
	}
	args = append(s.credentialArgs(b.SocketID, url), args...)
	_, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: b.Path, Args: args, Timeout: 2 * time.Hour, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	if err != nil {
		writeError(w, err)
		return
	}
	p, _ := filepath.Abs(filepath.Join(b.Path, dest))
	writeJSON(w, http.StatusOK, map[string]string{"path": p})
}

func (s *Server) postPush(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path         string `json:"path"`
		Remote       string `json:"remote"`
		RefSpec      string `json:"refSpec"`
		RemoteBranch string `json:"remoteBranch"`
		Force        bool   `json:"force"`
		SocketID     any    `json:"socketId"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	if _, ok := s.requireSocket(w, b.SocketID); !ok {
		return
	}
	ref := b.RefSpec
	if ref == "" {
		ref = "HEAD"
	}
	if b.RemoteBranch != "" {
		ref += ":" + b.RemoteBranch
	}
	args := []string{"push", b.Remote, ref}
	if b.Force {
		args = append(args, "-f")
	}
	args = append(s.credentialArgs(b.SocketID, b.Remote), args...)
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: b.Path, Args: args, Timeout: 10 * time.Minute, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	writeResult(w, string(res.Stdout), err)
}

func (s *Server) postReset(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		To   string `json:"to"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.AutoStashExecuteAndPop(r.Context(), b.Path, []string{"reset", "--" + b.Mode, b.To}, 0)
	runWriteResult(w, out, err)
}

func (s *Server) postDiscardChanges(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		All  bool   `json:"all"`
		File string `json:"file"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	var out string
	var err error
	if b.All {
		out, err = s.git.DiscardAllChanges(r.Context(), b.Path)
	} else {
		out, err = s.git.DiscardChangesInFile(r.Context(), b.Path, strings.TrimSpace(b.File))
	}
	runWriteResult(w, out, err)
}

func (s *Server) postIgnoreFile(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	lineEnding := "\n"
	if runtime.GOOS == "windows" {
		lineEnding = "\r\n"
	}
	f, err := os.OpenFile(filepath.Join(strings.TrimSpace(b.Path), ".gitignore"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err == nil {
		_, err = f.WriteString(lineEnding + strings.TrimSpace(b.File))
		_ = f.Close()
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"errorCode": "error-appending-ignore", "error": "Error while appending to .gitignore file."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) postCommit(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Path        string              `json:"path"`
		Amend       bool                `json:"amend"`
		EmptyCommit bool                `json:"emptyCommit"`
		Message     *string             `json:"message"`
		Files       []gitapi.CommitFile `json:"files"`
	}
	if !decodeJSONBody(w, r, &raw) || !ensureExistingPath(w, raw.Path) {
		return
	}
	out, err := s.git.Commit(r.Context(), raw.Path, raw.Amend, raw.EmptyCommit, raw.Message, raw.Files)
	runWriteResult(w, out, err)
}

func (s *Server) postRevert(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path   string `json:"path"`
		Commit string `json:"commit"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: b.Path, Args: []string{"revert", b.Commit}})
	if err != nil {
		var ge *gitapi.Error
		if errors.As(err, &ge) && strings.Contains(ge.Message, "is a merge but no -m option was given.") {
			res, err = s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: b.Path, Args: []string{"revert", "-m", "1", b.Commit}})
		}
	}
	writeResult(w, string(res.Stdout), err)
}

func (s *Server) postBranch(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
		Name  string `json:"name"`
		SHA1  string `json:"sha1"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	sha := strings.TrimSpace(b.SHA1)
	if sha == "" {
		sha = "HEAD"
	}
	args := []string{"branch"}
	if b.Force {
		args = append(args, "-f")
	}
	args = append(args, strings.TrimSpace(b.Name), sha)
	out, err := s.git.RunMutation(r.Context(), b.Path, args...)
	runWriteResult(w, out, err)
}

func (s *Server) deleteBranch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !ensureExistingPath(w, q.Get("path")) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), q.Get("path"), "branch", "-D", strings.TrimSpace(q.Get("name")))
	runWriteResult(w, out, err)
}

func (s *Server) deleteRemoteBranch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !ensureExistingPath(w, q.Get("path")) {
		return
	}
	if _, ok := s.requireSocket(w, q.Get("socketId")); !ok {
		return
	}
	args := append(s.credentialArgs(q.Get("socketId"), q.Get("remote")), "push", q.Get("remote"), ":"+strings.TrimSpace(q.Get("name")))
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: q.Get("path"), Args: args, Timeout: 10 * time.Minute, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	if err != nil {
		var ge *gitapi.Error
		if errors.As(err, &ge) && strings.Contains(ge.Stderr, "remote ref does not exist") {
			err = nil
		}
	}
	writeResult(w, string(res.Stdout), err)
}

func (s *Server) postTag(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
		Name  string `json:"name"`
		SHA1  string `json:"sha1"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	sha := strings.TrimSpace(b.SHA1)
	if sha == "" {
		sha = "HEAD"
	}
	args := []string{"tag"}
	if b.Force {
		args = append(args, "-f")
	}
	if s.cfg.IsForceGPGSign {
		args = append(args, "-s")
	} else {
		args = append(args, "-a")
	}
	name := strings.TrimSpace(b.Name)
	args = append(args, name, "-m", name, sha)
	out, err := s.git.RunMutation(r.Context(), b.Path, args...)
	runWriteResult(w, out, err)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !ensureExistingPath(w, q.Get("path")) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), q.Get("path"), "tag", "-d", strings.TrimSpace(q.Get("name")))
	runWriteResult(w, out, err)
}

func (s *Server) deleteRemoteTag(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !ensureExistingPath(w, q.Get("path")) {
		return
	}
	// The Node backend ignores failure of the local delete before deleting remote.
	_, _ = s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: q.Get("path"), Args: []string{"tag", "-d", strings.TrimSpace(q.Get("name"))}})
	args := append(s.credentialArgs(q.Get("socketId"), q.Get("remote")), "push", q.Get("remote"), ":refs/tags/"+strings.TrimSpace(q.Get("name")))
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: q.Get("path"), Args: args, Timeout: 10 * time.Minute, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	writeResult(w, string(res.Stdout), err)
}

func (s *Server) postCheckout(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		Name string `json:"name"`
		SHA1 string `json:"sha1"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	args := []string{"checkout", strings.TrimSpace(b.Name)}
	if b.SHA1 != "" {
		args = []string{"checkout", "-b", strings.TrimSpace(b.Name), b.SHA1}
	}
	out, err := s.git.AutoStashExecuteAndPop(r.Context(), b.Path, args, 0)
	runWriteResult(w, out, err)
}

func (s *Server) postCherryPick(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.AutoStashExecuteAndPop(r.Context(), b.Path, []string{"cherry-pick", strings.TrimSpace(b.Name)}, 0)
	runWriteResult(w, out, err)
}

func (s *Server) postRemote(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		URL  string `json:"url"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), b.Path, "remote", "add", r.PathValue("name"), b.URL)
	runWriteResult(w, out, err)
}

func (s *Server) deleteRemote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !ensureExistingPath(w, q.Get("path")) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), q.Get("path"), "remote", "remove", r.PathValue("name"))
	runWriteResult(w, out, err)
}

func (s *Server) postMerge(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		With string `json:"with"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	args := []string{"merge"}
	if s.cfg.NoFFMerge {
		args = append(args, "--no-ff")
	}
	args = append(args, strings.TrimSpace(b.With))
	out, err := s.git.RunMutation(r.Context(), b.Path, args...)
	runWriteResult(w, out, err)
}

func (s *Server) postMergeContinue(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: b.Path, Args: []string{"commit", "--file=-"}, Stdin: []byte(b.Message)})
	writeResult(w, string(res.Stdout), err)
}

func (s *Server) postMergeAbort(w http.ResponseWriter, r *http.Request) {
	path, ok := decodePathOnly(w, r)
	if !ok {
		return
	}
	out, err := s.git.RunMutation(r.Context(), path, "merge", "--abort")
	runWriteResult(w, out, err)
}

func (s *Server) postSquash(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), b.Path, "merge", "--squash", strings.TrimSpace(b.Target))
	runWriteResult(w, out, err)
}

func (s *Server) postRebase(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		Onto string `json:"onto"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), b.Path, "rebase", strings.TrimSpace(b.Onto))
	runWriteResult(w, out, err)
}

func (s *Server) postRebaseContinue(w http.ResponseWriter, r *http.Request) {
	path, ok := decodePathOnly(w, r)
	if !ok {
		return
	}
	out, err := s.git.RunMutation(r.Context(), path, "rebase", "--continue")
	runWriteResult(w, out, err)
}

func (s *Server) postRebaseAbort(w http.ResponseWriter, r *http.Request) {
	path, ok := decodePathOnly(w, r)
	if !ok {
		return
	}
	out, err := s.git.RunMutation(r.Context(), path, "rebase", "--abort")
	runWriteResult(w, out, err)
}

func decodePathOnly(w http.ResponseWriter, r *http.Request) (string, bool) {
	var b struct {
		Path string `json:"path"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return "", false
	}
	return b.Path, true
}

func (s *Server) postResolveConflicts(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path  string   `json:"path"`
		Files []string `json:"files"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.ResolveConflicts(r.Context(), b.Path, b.Files)
	runWriteResult(w, out, err)
}

func (s *Server) postLaunchMergeTool(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		File string `json:"file"`
		Tool any    `json:"tool"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	args := []string{"mergetool"}
	if tool, ok := b.Tool.(string); ok {
		// Preserve the historical argument spelling from Node Ungit.
		args = append(args, "--tool ", tool)
	}
	args = append(args, "--no-prompt", b.File)
	go func(path string, a []string) {
		_, _ = s.git.Runner.Run(context.Background(), gitapi.Command{RepoPath: path, Args: a})
	}(b.Path, append([]string(nil), args...))
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) postSubmodulesUpdate(w http.ResponseWriter, r *http.Request) {
	path, ok := decodePathOnly(w, r)
	if !ok {
		return
	}
	if _, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: path, Args: []string{"submodule", "init"}}); err != nil {
		writeError(w, err)
		return
	}
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: path, Args: []string{"submodule", "update"}})
	writeResult(w, string(res.Stdout), err)
}

func (s *Server) postSubmodulesAdd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path          string `json:"path"`
		SubmoduleURL  string `json:"submoduleUrl"`
		SubmodulePath string `json:"submodulePath"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), b.Path, "submodule", "add", strings.TrimSpace(b.SubmoduleURL), strings.TrimSpace(b.SubmodulePath))
	runWriteResult(w, out, err)
}

func (s *Server) deleteSubmodule(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	if !ensureExistingPath(w, path) {
		return
	}
	name := q.Get("submoduleName")
	subPath := q.Get("submodulePath")
	if _, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: path, Args: []string{"submodule", "deinit", "-f", name}}); err != nil {
		writeError(w, err)
		return
	}
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: path, Args: []string{"rm", "-f", name}})
	if err != nil {
		writeError(w, err)
		return
	}
	if err := os.RemoveAll(filepath.Join(path, subPath)); err != nil {
		writeError(w, err)
		return
	}
	if err := os.RemoveAll(filepath.Join(path, ".git", "modules", subPath)); err != nil {
		writeError(w, err)
		return
	}
	writeResult(w, string(res.Stdout), nil)
}

func (s *Server) postStash(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	out, err := s.git.RunMutation(r.Context(), b.Path, "stash", "save", "--include-untracked", b.Message)
	runWriteResult(w, out, err)
}

func (s *Server) deleteStash(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !ensureExistingPath(w, q.Get("path")) {
		return
	}
	typeName := "drop"
	if q.Get("apply") == "true" {
		typeName = "apply"
	}
	out, err := s.git.RunMutation(r.Context(), q.Get("path"), "stash", typeName, fmt.Sprintf("stash@{%s}", r.PathValue("id")))
	runWriteResult(w, out, err)
}

func (s *Server) postCreateDir(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Dir string `json:"dir"`
	}
	// Node accepts dir from either query or JSON body.
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		dir = b.Dir
	}
	if dir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"errorCode": "missing-request-parameter", "error": "You need to supply the path request parameter"})
		return
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) putGitIgnore(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string  `json:"path"`
		Data *string `json:"data"`
	}
	if !decodeJSONBody(w, r, &b) || !ensureExistingPath(w, b.Path) {
		return
	}
	if b.Data == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Invalid .gitignore content"})
		return
	}
	if err := os.WriteFile(filepath.Join(b.Path, ".gitignore"), []byte(*b.Data), 0o666); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
