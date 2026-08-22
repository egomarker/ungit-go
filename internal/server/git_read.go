package server

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	gitapi "github.com/egomarker/ungit-go/internal/git"
)

func (s *Server) registerReadAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fs/exists", s.fsExists)
	mux.HandleFunc("GET /api/fs/listDirectories", s.fsListDirectories)
	mux.HandleFunc("GET /api/quickstatus", s.quickStatus)
	mux.HandleFunc("GET /api/status", s.withExistingPath(s.getStatus))
	mux.HandleFunc("GET /api/gitlog", s.withExistingPath(s.getGitLog))
	mux.HandleFunc("GET /api/show", s.getShow)
	mux.HandleFunc("GET /api/head", s.withExistingPath(s.getHead))
	mux.HandleFunc("GET /api/refs", s.withExistingPath(s.getRefs))
	mux.HandleFunc("GET /api/branches", s.withExistingPath(s.getBranches))
	mux.HandleFunc("GET /api/tags", s.withExistingPath(s.getTags))
	mux.HandleFunc("GET /api/checkout", s.withExistingPath(s.getCheckout))
	mux.HandleFunc("GET /api/remotes", s.withExistingPath(s.getRemotes))
	mux.HandleFunc("GET /api/remotes/{name}", s.withExistingPath(s.getRemote))
	mux.HandleFunc("GET /api/diff", s.withExistingPath(s.getDiff))
	mux.HandleFunc("GET /api/diff/image", s.withExistingPath(s.getDiffImage))
	mux.HandleFunc("GET /api/baserepopath", s.withExistingPath(s.getBaseRepoPath))
	mux.HandleFunc("GET /api/submodules", s.withExistingPath(s.getSubmodules))
	mux.HandleFunc("GET /api/stashes", s.withExistingPath(s.getStashes))
	mux.HandleFunc("GET /api/gitconfig", s.getGitConfig)
	mux.HandleFunc("GET /api/gitignore", s.withExistingPath(s.getGitIgnore))
	mux.HandleFunc("GET /api/fetch", s.withExistingPath(s.getFetch))
	mux.HandleFunc("GET /api/remote/tags", s.withExistingPath(s.getRemoteTags))
}

func (s *Server) withExistingPath(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		if p == "" {
			p = r.FormValue("path")
		}
		if _, err := os.Stat(p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "'No such path: " + p, "errorCode": "no-such-path"})
			return
		}
		next(w, r)
	}
}

func (s *Server) fsExists(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(r.URL.Query().Get("path"))
	writeJSON(w, http.StatusOK, err == nil)
}

func (s *Server) fsListDirectories(w http.ResponseWriter, r *http.Request) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		term = "."
	}
	dir, err := filepath.Abs(term)
	if err != nil {
		writeError(w, err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, err)
		return
	}
	dirs := []string{dir}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}
	writeJSON(w, http.StatusOK, dirs)
}

func (s *Server) quickStatus(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if _, err := os.Stat(p); err != nil {
		writeJSON(w, http.StatusOK, gitapi.RevParseResult{Type: "no-such-path", GitRootPath: p})
		return
	}
	res := s.git.RevParse(r.Context(), p)
	if res.Type == "uninited" {
		subRepos := []string{}
		res.SubRepos = &subRepos
		entries, err := os.ReadDir(p)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				rp := s.git.RevParse(r.Context(), filepath.Join(p, e.Name()))
				if rp.Type == "inited" || rp.Type == "bare" {
					*res.SubRepos = append(*res.SubRepos, rp.GitRootPath)
				}
			}
			sort.Strings(*res.SubRepos)
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	res, err := s.git.Status(r.Context(), r.URL.Query().Get("path"), "")
	writeResult(w, res, err)
}

func (s *Server) getGitLog(w http.ResponseWriter, r *http.Request) {
	limit, err := gitapi.ParseInt(r.URL.Query().Get("limit"), s.cfg.NumberOfNodesPerLoad)
	if err != nil {
		writeError(w, err)
		return
	}
	skip, err := gitapi.ParseInt(r.URL.Query().Get("skip"), 0)
	if err != nil {
		writeError(w, err)
		return
	}
	res, err := s.git.Log(r.Context(), r.URL.Query().Get("path"), limit, skip, s.cfg.MaxActiveBranchSearchIteration)
	if gitapi.IsGitErrorCode(err, "no-head", "no-commits", "not-a-repository") {
		writeJSON(w, http.StatusOK, gitapi.LogResult{Limit: limit, Skip: skip, Nodes: []gitapi.Commit{}})
		return
	}
	writeResult(w, res, err)
}

func (s *Server) getShow(w http.ResponseWriter, r *http.Request) {
	text, err := s.git.Runner.RunText(r.Context(), r.URL.Query().Get("path"), "show", "--numstat", "-z", r.URL.Query().Get("sha1"))
	if err != nil {
		writeError(w, err)
		return
	}
	commits, _ := gitapi.ParseGitLog(text)
	writeJSON(w, http.StatusOK, commits)
}

func (s *Server) getHead(w http.ResponseWriter, r *http.Request) {
	res, err := s.git.Head(r.Context(), r.URL.Query().Get("path"))
	if gitapi.IsGitErrorCode(err, "no-head", "no-commits", "not-a-repository") {
		writeJSON(w, http.StatusOK, []gitapi.Commit{})
		return
	}
	writeResult(w, res, err)
}

func (s *Server) getRefs(w http.ResponseWriter, r *http.Request) {
	repoPath := r.URL.Query().Get("path")
	if r.URL.Query().Get("remoteFetch") != "" {
		remoteText, remoteErr := s.git.Runner.RunText(r.Context(), repoPath, "remote")
		if remoteErr == nil {
			for _, remote := range strings.Split(strings.TrimSpace(remoteText), "\n") {
				if remote == "" {
					continue
				}
				// upstream Ungit deliberately ignores fetch errors here, most commonly credentials/offline failures.
				args := append(s.credentialArgs(r.URL.Query().Get("socketId"), remote), "fetch", remote)
				_, _ = s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: repoPath, Args: args, Timeout: 10 * time.Minute, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
			}
		}
	}
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: repoPath, Args: []string{"show-ref", "-d"}})
	if err != nil {
		var ge *gitapi.Error
		if errors.As(err, &ge) && ge.Message == "" {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, err)
		return
	}
	refs := []map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(res.Stdout)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		sha, name := parts[0], parts[1]
		if strings.Contains(name, "refs/tags") && strings.Contains(name, "^{}") && len(refs) > 0 {
			refs[len(refs)-1]["sha1"] = sha
		} else {
			refs = append(refs, map[string]string{"name": name, "sha1": sha})
		}
	}
	writeJSON(w, http.StatusOK, refs)
}

func (s *Server) getBranches(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("isLocalBranchOnly") == "false"
	arg := ""
	if all {
		arg = "-a"
	}
	text, err := s.git.Runner.RunText(r.Context(), r.URL.Query().Get("path"), "branch", arg)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gitapi.ParseGitBranches(text))
}
func (s *Server) getTags(w http.ResponseWriter, r *http.Request) {
	text, err := s.git.Runner.RunText(r.Context(), r.URL.Query().Get("path"), "tag", "-l")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gitapi.ParseGitTags(text))
}
func (s *Server) getCheckout(w http.ResponseWriter, r *http.Request) {
	b, err := s.git.CurrentBranch(r.Context(), r.URL.Query().Get("path"))
	writeResult(w, b, err)
}
func (s *Server) getRemotes(w http.ResponseWriter, r *http.Request) {
	text, err := s.git.Runner.RunText(r.Context(), r.URL.Query().Get("path"), "remote", "-v")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gitapi.ParseGitRemotes(text))
}
func (s *Server) getRemote(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	text, err := s.git.Runner.RunText(r.Context(), r.URL.Query().Get("path"), "config", "--get", "remote."+name+".url")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, parseAddress(strings.Split(text, "\n")[0]))
}

func (s *Server) getDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text, err := s.git.DiffFile(r.Context(), q.Get("path"), q.Get("file"), q.Get("oldFile"), q.Get("sha1"), q.Get("whiteSpace") == "true")
	writeResult(w, text, err)
}

func (s *Server) getDiffImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filename := q.Get("filename")
	if ct := mime.TypeByExtension(filepath.Ext(filename)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if q.Get("version") == "current" {
		http.ServeFile(w, r, filepath.Join(q.Get("path"), filename))
		return
	}
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: q.Get("path"), Args: []string{"show", q.Get("version") + ":" + filename}, Stdout: w})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = res
}

func (s *Server) getBaseRepoPath(w http.ResponseWriter, r *http.Request) {
	current := filepath.Clean(filepath.Join(r.URL.Query().Get("path"), ".."))
	text, err := s.git.Runner.RunText(r.Context(), current, "rev-parse", "--show-toplevel")
	if gitapi.IsGitErrorCode(err, "not-a-repository", "must-be-in-working-tree") {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	p, _ := filepath.Abs(strings.TrimSpace(text))
	writeJSON(w, http.StatusOK, map[string]string{"path": p})
}

func (s *Server) getSubmodules(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(filepath.Join(r.URL.Query().Get("path"), ".gitmodules"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, gitapi.ParseGitSubmodule(string(b)))
}
func (s *Server) getStashes(w http.ResponseWriter, r *http.Request) {
	text, err := s.git.Runner.RunText(r.Context(), r.URL.Query().Get("path"), "stash", "list", "--decorate=full", "--pretty=fuller", "-z", "--parents", "--numstat")
	if err != nil {
		writeError(w, err)
		return
	}
	commits, _ := gitapi.ParseGitLog(text)
	writeJSON(w, http.StatusOK, commits)
}
func (s *Server) getGitConfig(w http.ResponseWriter, r *http.Request) {
	text, err := s.git.Runner.RunText(r.Context(), "", "config", "--list")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gitapi.ParseGitConfig(text))
}
func (s *Server) getGitIgnore(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(filepath.Join(r.URL.Query().Get("path"), ".gitignore"))
	if os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, map[string]string{"content": ""})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(b)})
}

func (s *Server) getFetch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, ok := s.requireSocket(w, q.Get("socketId")); !ok {
		return
	}
	args := []string{"fetch"}
	if s.cfg.AutoPruneOnFetch {
		args = append(args, "--prune")
	}
	args = append(args, "--", q.Get("remote"))
	if q.Get("ref") != "" {
		args = append(args, q.Get("ref"))
	}
	args = append(s.credentialArgs(q.Get("socketId"), q.Get("remote")), args...)
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: q.Get("path"), Args: args, Timeout: 10 * time.Minute, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	if err != nil {
		writeError(w, err)
		return
	}
	writeResult(w, string(res.Stdout), nil)
}

func (s *Server) getRemoteTags(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, ok := s.requireSocket(w, q.Get("socketId")); !ok {
		return
	}
	args := append(s.credentialArgs(q.Get("socketId"), q.Get("remote")), "ls-remote", "--tags", q.Get("remote"))
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: q.Get("path"), Args: args, Timeout: 10 * time.Minute, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	if err != nil {
		writeError(w, err)
		return
	}
	out := []map[string]string{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if line == "" || strings.HasPrefix(line, "From ") {
			continue
		}
		sha := ""
		name := ""
		if len(line) >= 40 {
			sha = line[:40]
		}
		if len(line) > 41 {
			name = strings.TrimSpace(line[41:])
		}
		out = append(out, map[string]string{"sha1": sha, "name": name, "remote": q.Get("remote")})
	}
	writeJSON(w, http.StatusOK, out)
}

func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	if value == nil {
		value = map[string]any{}
	}
	if text, ok := value.(string); ok && text == "" {
		value = map[string]any{}
	}
	writeJSON(w, http.StatusOK, value)
}
func writeError(w http.ResponseWriter, err error) {
	var ge *gitapi.Error
	if errors.As(err, &ge) {
		writeJSON(w, http.StatusBadRequest, ge)
		return
	}
	var se *gitapi.SimpleError
	if errors.As(err, &se) {
		writeJSON(w, http.StatusBadRequest, se)
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

var (
	sshPortRE = regexp.MustCompile(`^ssh://(.*):(\d*)/(.*)$`)
	sshRE     = regexp.MustCompile(`^ssh://([^/]*)/(.*)$`)
	scpUserRE = regexp.MustCompile(`^([^@]*)@([^:]*):(.+?)(?:\.git)?$`)
	scpRE     = regexp.MustCompile(`^([^:]*):(.+?)(?:\.git)?$`)
	httpsRE   = regexp.MustCompile(`^https://([^/]*)/(.+?)(?:\.git)?$`)
)

func parseAddress(remote string) map[string]string {
	out := map[string]string{"address": remote}
	projectFields := func(project string) {
		project = strings.TrimSuffix(project, ".git")
		out["project"] = project
		parts := strings.Split(project, "/")
		out["shortProject"] = parts[len(parts)-1]
	}
	if regexp.MustCompile(`^[A-Za-z]:\\`).MatchString(remote) {
		out["host"] = "localhost"
		p := strings.TrimRight(remote, `\\`)
		parts := strings.Split(p, `\`)
		projectFields(parts[len(parts)-1])
		return out
	}
	if filepath.IsAbs(remote) || strings.HasPrefix(remote, "~/") {
		out["host"] = "localhost"
		projectFields(filepath.Base(strings.TrimRight(remote, "/")))
		return out
	}
	if m := sshPortRE.FindStringSubmatch(remote); len(m) > 0 {
		out["host"], out["port"] = m[1], m[2]
		projectFields(m[3])
		return out
	}
	if m := sshRE.FindStringSubmatch(remote); len(m) > 0 {
		out["host"] = m[1]
		projectFields(m[2])
		return out
	}
	// Check HTTPS before generic host:path syntax, otherwise "https:" is
	// incorrectly interpreted as an scp-like host.
	if m := httpsRE.FindStringSubmatch(remote); len(m) > 0 {
		out["host"] = m[1]
		projectFields(m[2])
		return out
	}
	if m := scpUserRE.FindStringSubmatch(remote); len(m) > 0 {
		out["username"], out["host"] = m[1], m[2]
		projectFields(m[3])
		return out
	}
	if m := scpRE.FindStringSubmatch(remote); len(m) > 0 {
		out["host"] = m[1]
		projectFields(m[2])
		return out
	}
	if strings.Contains(remote, "/") {
		out["host"] = "localhost"
		projectFields(filepath.Base(strings.TrimRight(remote, "/")))
	}
	return out
}
