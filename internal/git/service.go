package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/egomarker/ungit-go/internal/config"
)

const (
	emptyTreeSHA1   = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	emptyTreeSHA256 = "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321"
)

type Service struct {
	cfg    config.Config
	Runner *Runner
}

func NewService(cfg config.Config) *Service { return &Service{cfg: cfg, Runner: NewRunner(cfg)} }

func (s *Service) optionalLockArg() string {
	if s.Runner.optionalLocks {
		return "--no-optional-locks"
	}
	return ""
}

func (s *Service) Status(ctx context.Context, repoPath, file string) (Status, error) {
	stagedText, err := s.Runner.RunText(ctx, repoPath, s.optionalLockArg(), "diff", "--numstat", "--cached", "-z", "--", file)
	if err != nil {
		return Status{}, err
	}
	unstaged := map[string]NumStat{}
	if s.cfg.IsEnableNumStat {
		unstagedText, err := s.Runner.RunText(ctx, repoPath, s.optionalLockArg(), "diff", "--numstat", "-z", "--", file)
		if err != nil {
			return Status{}, err
		}
		unstaged = ParseGitStatusNumstat(unstagedText)
	}
	statusText, err := s.Runner.RunText(ctx, repoPath, s.optionalLockArg(), "status", "-s", "-b", "-u", "-z", file)
	if err != nil {
		return Status{}, err
	}
	status := ParseGitStatus(statusText)

	gitDir := filepath.Join(repoPath, ".git")
	status.InRebase = pathExists(filepath.Join(gitDir, "rebase-merge")) || pathExists(filepath.Join(gitDir, "rebase-apply"))
	status.InMerge = pathExists(filepath.Join(gitDir, "MERGE_HEAD"))
	status.InCherry = pathExists(filepath.Join(gitDir, "CHERRY_PICK_HEAD"))
	if status.InMerge || status.InCherry {
		if b, readErr := os.ReadFile(filepath.Join(gitDir, "MERGE_MSG")); readErr == nil {
			status.CommitMessage = string(b)
		} else {
			status.InMerge, status.InCherry = false, false
		}
	}

	numstats := ParseGitStatusNumstat(stagedText)
	for k, v := range unstaged {
		numstats[k] = v
	}
	for name, f := range status.Files {
		absoluteName := strings.ReplaceAll(name, "../", "")
		stats, ok := numstats[absoluteName]
		if ok {
			f.Additions, f.Deletions = stats.Additions, stats.Deletions
		} else {
			f.Additions, f.Deletions = "-", "-"
		}
		if f.Conflict {
			status.InConflict = true
		}
		status.Files[name] = f
	}
	return status, nil
}

func pathExists(path string) bool { _, err := os.Stat(path); return err == nil }

type RevParseResult struct {
	Type        string    `json:"type"`
	GitRootPath string    `json:"gitRootPath"`
	SubRepos    *[]string `json:"subRepos,omitempty"`
}

func (s *Service) RevParse(ctx context.Context, repoPath string) RevParseResult {
	result, err := s.Runner.RunText(ctx, repoPath, "rev-parse", "--is-inside-work-tree", "--is-bare-repository")
	if err != nil {
		return RevParseResult{Type: "uninited", GitRootPath: filepath.Clean(repoPath)}
	}
	lines := strings.Split(result, "\n")
	if len(lines) > 1 && strings.Contains(lines[1], "true") {
		return RevParseResult{Type: "bare", GitRootPath: repoPath}
	}
	top, err := s.Runner.RunText(ctx, repoPath, "rev-parse", "--show-toplevel")
	root := repoPath
	if err == nil && strings.TrimSpace(top) != "" {
		root = filepath.Clean(strings.TrimSpace(top))
	} else {
		root = filepath.Clean(root)
	}
	if len(lines) > 0 && strings.Contains(lines[0], "true") {
		return RevParseResult{Type: "inited", GitRootPath: root}
	}
	return RevParseResult{Type: "uninited", GitRootPath: root}
}

type LogResult struct {
	Limit       int      `json:"limit"`
	Skip        int      `json:"skip"`
	Nodes       []Commit `json:"nodes"`
	IsHeadExist *bool    `json:"isHeadExist,omitempty"`
}

func (s *Service) Log(ctx context.Context, repoPath string, limit, skip, maxActiveBranchSearchIteration int) (LogResult, error) {
	text, err := s.Runner.RunText(ctx, repoPath,
		"log", "--cc", "--decorate=full", "--show-signature", "--date=default", "--pretty=fuller", "-z", "--branches", "--tags", "--remotes", "--parents", "--no-notes", "--numstat", "--date-order",
		fmt.Sprintf("--max-count=%d", limit), fmt.Sprintf("--skip=%d", skip))
	if err != nil {
		return LogResult{}, err
	}
	nodes, head := ParseGitLog(text)
	if maxActiveBranchSearchIteration > 0 && !head && len(nodes) > 0 {
		inner, innerErr := s.Log(ctx, repoPath, s.cfg.NumberOfNodesPerLoad+limit, s.cfg.NumberOfNodesPerLoad+skip, maxActiveBranchSearchIteration-1)
		if innerErr != nil {
			return LogResult{}, innerErr
		}
		innerHead := inner.IsHeadExist != nil && *inner.IsHeadExist
		newLimit, newSkip := limit, skip
		if !innerHead {
			newLimit += s.cfg.NumberOfNodesPerLoad
			newSkip += s.cfg.NumberOfNodesPerLoad
		}
		all := append(nodes, inner.Nodes...)
		return LogResult{Limit: newLimit, Skip: newSkip, Nodes: all, IsHeadExist: &innerHead}, nil
	}
	if len(nodes) == 0 {
		return LogResult{Limit: limit, Skip: skip, Nodes: nodes}, nil
	}
	return LogResult{Limit: limit, Skip: skip, Nodes: nodes, IsHeadExist: &head}, nil
}

func (s *Service) Head(ctx context.Context, repoPath string) ([]Commit, error) {
	text, err := s.Runner.RunText(ctx, repoPath, "log", "--decorate=full", "--pretty=fuller", "-z", "--parents", "--max-count=1")
	if err != nil {
		return nil, err
	}
	commits, _ := ParseGitLog(text)
	return commits, nil
}

func (s *Service) CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	text, err := s.Runner.RunText(ctx, repoPath, "branch")
	if err != nil {
		return "", err
	}
	for _, b := range ParseGitBranches(text) {
		if b.Current {
			return b.Name, nil
		}
	}
	return "", nil
}

func (s *Service) DiffFile(ctx context.Context, repoPath, filename, oldFilename, sha1 string, ignoreWhitespace bool) (string, error) {
	white := ""
	if ignoreWhitespace {
		white = "-w"
	}
	if sha1 != "" {
		initial, err := s.Runner.RunText(ctx, repoPath, "rev-list", "--max-parents=0", sha1)
		if err != nil {
			return "", err
		}
		prev := sha1 + "^"
		if strings.TrimSpace(initial) == sha1 {
			if len(sha1) == 64 {
				prev = emptyTreeSHA256
			} else {
				prev = emptyTreeSHA1
			}
		}
		if oldFilename != "" && oldFilename != filename {
			return s.Runner.RunText(ctx, repoPath, "diff", white, prev+":"+strings.TrimSpace(oldFilename), sha1+":"+strings.TrimSpace(filename))
		}
		return s.Runner.RunText(ctx, repoPath, "diff", white, prev, sha1, "--", strings.TrimSpace(filename))
	}
	rp := s.RevParse(ctx, repoPath)
	if rp.Type == "bare" {
		return "", nil
	}
	st, err := s.Status(ctx, repoPath, "")
	if err != nil {
		return "", err
	}
	f, ok := st.Files[filename]
	if !ok {
		if _, statErr := os.Stat(filepath.Join(repoPath, filename)); statErr == nil {
			return "", nil
		}
		return "", &SimpleError{ErrorCode: "no-such-file", ErrorText: "No such file: " + filename}
	}
	if f.IsNew {
		null := "/dev/null"
		if runtime.GOOS == "windows" {
			null = "NUL"
		}
		res, e := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"diff", "--no-index", null, strings.TrimSpace(filename)}, AllowError: true})
		return string(res.Stdout), e
	}
	if f.Renamed {
		return s.Runner.RunText(ctx, repoPath, "diff", white, "HEAD:"+oldFilename, strings.TrimSpace(filename))
	}
	return s.Runner.RunText(ctx, repoPath, "diff", white, "HEAD", "--", strings.TrimSpace(filename))
}

type SimpleError struct {
	ErrorCode string `json:"errorCode"`
	ErrorText string `json:"error"`
}

func (e *SimpleError) Error() string { return e.ErrorText }

func IsGitErrorCode(err error, codes ...string) bool {
	var ge *Error
	if !errors.As(err, &ge) {
		return false
	}
	for _, c := range codes {
		if ge.ErrorCode == c {
			return true
		}
	}
	return false
}

func ParseInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	// Match JavaScript parseInt used by the Node API: trim leading space,
	// consume an optional sign and the leading decimal digit run, and ignore
	// trailing non-digits.
	s := strings.TrimSpace(value)
	if s == "" {
		return 0, &SimpleError{ErrorText: "invalid number"}
	}
	i := 0
	if s[0] == '+' || s[0] == '-' {
		i++
	}
	startDigits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == startDigits {
		return 0, &SimpleError{ErrorText: "invalid number"}
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, &SimpleError{ErrorText: "invalid number"}
	}
	return n, nil
}
