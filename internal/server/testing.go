package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	gitapi "github.com/egomarker/ungit-go/internal/git"
)

func (s *Server) registerTestingAPI(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/testing/createtempdir", s.testingCreateTempDir)
	mux.HandleFunc("POST /api/testing/createfile", s.testingWriteFile)
	mux.HandleFunc("POST /api/testing/changefile", s.testingWriteFile)
	mux.HandleFunc("POST /api/testing/createimagefile", s.testingWriteImageFile)
	mux.HandleFunc("POST /api/testing/changeimagefile", s.testingWriteImageFile)
	mux.HandleFunc("POST /api/testing/removefile", s.testingRemoveFile)
	mux.HandleFunc("POST /api/testing/git", s.testingGit)
	mux.HandleFunc("POST /api/testing/cleanup", s.testingCleanup)
}

func (s *Server) testingCreateTempDir(w http.ResponseWriter, r *http.Request) {
	dir, err := os.MkdirTemp("", "test-temp-dir")
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	s.testTempMu.Lock()
	s.testTempDirs = append(s.testTempDirs, dir)
	s.testTempMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"path": filepath.Clean(dir)})
}

func (s *Server) testingWriteFile(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path    string `json:"path"`
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if !decodeJSONBody(w, r, &b) {
		return
	}
	content := b.Content
	if content == "" {
		content = "test content\n"
	}
	if err := os.WriteFile(b.File, []byte(content), 0o666); err != nil {
		writeError(r.Context(), w, err)
		return
	}
	s.realtime.broadcast(b.Path, "working-tree-changed", map[string]string{"repository": b.Path})
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) testingWriteImageFile(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if !decodeJSONBody(w, r, &b) {
		return
	}
	content := []byte("png")
	if r.URL.Path == "/api/testing/changeimagefile" {
		content = []byte("png ~~")
	}
	if err := os.WriteFile(b.File, content, 0o666); err != nil {
		writeError(r.Context(), w, err)
		return
	}
	s.realtime.broadcast(b.Path, "working-tree-changed", map[string]string{"repository": b.Path})
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) testingRemoveFile(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if !decodeJSONBody(w, r, &b) {
		return
	}
	if err := os.Remove(b.File); err != nil {
		writeError(r.Context(), w, err)
		return
	}
	s.realtime.broadcast(b.Path, "working-tree-changed", map[string]string{"repository": b.Path})
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) testingGit(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Path    string          `json:"path"`
		Command json.RawMessage `json:"command"`
	}
	if !decodeJSONBody(w, r, &raw) {
		return
	}
	var args []string
	if err := json.Unmarshal(raw.Command, &args); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res, err := s.git.Runner.Run(r.Context(), gitapi.Command{RepoPath: raw.Path, Args: args})
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	s.realtime.broadcast(raw.Path, "working-tree-changed", map[string]string{"repository": raw.Path})
	writeResult(r.Context(), w, string(res.Stdout), nil)
}

func (s *Server) testingCleanup(w http.ResponseWriter, r *http.Request) {
	s.testTempMu.Lock()
	dirs := append([]string(nil), s.testTempDirs...)
	s.testTempDirs = nil
	s.testTempMu.Unlock()
	cleaned := 0
	for _, dir := range dirs {
		if os.RemoveAll(dir) == nil {
			cleaned++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"result": cleaned})
}
