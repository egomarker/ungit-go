package server

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var latestVersionLookup = fetchLatestUngitGoVersion

func (s *Server) latestVersion(w http.ResponseWriter, r *http.Request) {
	current := s.version
	latest := current
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if got, err := latestVersionLookup(ctx); err == nil && got != "" {
		latest = got
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"latestVersion":  latest,
		"currentVersion": current,
		"outdated":       majorMinorGreater(latest, current),
	})
}

func fetchLatestUngitGoVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/egomarker/ungit-go/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Ungit-Go")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &httpError{Status: resp.Status}
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimPrefix(payload.TagName, "v"), nil
}

type httpError struct{ Status string }

func (e *httpError) Error() string { return e.Status }

var semverPrefix = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

// The inherited UI only shows its update banner for a major/minor bump. A
// prerelease/dev current version is treated conservatively.
func majorMinorGreater(latest, current string) bool {
	lm := semverPrefix.FindStringSubmatch(latest)
	cm := semverPrefix.FindStringSubmatch(current)
	if lm == nil || cm == nil || len(cm[0]) != len(current) && current[len(cm[0]):] != "" {
		if len(current) >= 4 && current[:4] == "dev-" {
			return false
		}
	}
	if lm == nil || cm == nil {
		return false
	}
	lmaj, _ := strconv.Atoi(lm[1])
	lmin, _ := strconv.Atoi(lm[2])
	cmaj, _ := strconv.Atoi(cm[1])
	cmin, _ := strconv.Atoi(cm[2])
	return lmaj > cmaj || (lmaj == cmaj && lmin > cmin)
}
