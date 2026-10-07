package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	ungitgoassets "github.com/egomarker/ungit-go"
	"github.com/egomarker/ungit-go/internal/config"
	gitinfo "github.com/egomarker/ungit-go/internal/git"
)

const pluginAPIVersion = "0.2.0"

type Server struct {
	cfg                config.Config
	git                *gitinfo.Service
	version            string
	indexHTML          []byte
	handler            http.Handler
	auth               *authManager
	realtime           *realtimeHub
	credentialMu       sync.RWMutex
	credentialEndpoint string
	testTempMu         sync.Mutex
	testTempDirs       []string
	metrics            serverMetrics
}

func New(cfg config.Config) (*Server, error) {
	version, err := packageVersion()
	if err != nil {
		return nil, err
	}
	indexHTML, err := buildIndex(cfg.RootPath)
	if err != nil {
		return nil, err
	}
	auth := newAuthManager(cfg.Authentication, cfg.Users)
	cfg.Users = nil // Match Node: never expose the authentication user map via serverdata.js.
	s := &Server{cfg: cfg, git: gitinfo.NewService(cfg), version: version, indexHTML: indexHTML, auth: auth}
	s.realtime = newRealtimeHub(s)
	s.handler = s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) Version() string       { return s.version }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	s.registerReadAPI(mux)
	s.registerWriteAPI(mux)
	if s.cfg.Dev {
		s.registerTestingAPI(mux)
	}
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{})
	})
	mux.HandleFunc("GET /api/gitversion", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, gitinfo.GetVersionInfo(ctx, s.cfg.GitBinPath))
	})
	mux.HandleFunc("GET /api/latestversion", s.latestVersion)
	if s.auth.enabled {
		mux.HandleFunc("POST /api/login", s.auth.login)
		mux.HandleFunc("GET /api/loggedin", s.auth.loggedIn)
		mux.HandleFunc("GET /api/logout", s.auth.logout)
	}
	mux.HandleFunc("GET /api/userconfig", s.getUserConfig)
	mux.HandleFunc("POST /api/userconfig", s.postUserConfig)
	mux.HandleFunc("GET /api/credentials", s.getCredentials)
	mux.HandleFunc("POST /api/client-log", s.postClientLog)
	mux.HandleFunc("GET /realtime/connect", s.realtime.handleConnect)
	mux.HandleFunc("POST /realtime/emit", s.realtime.handleEmit)
	mux.HandleFunc("GET /serverdata.js", s.serverData)
	mux.HandleFunc("GET /socket.io/socket.io.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write([]byte(socketIOCompatClient(s.cfg.RootPath)))
	})

	componentFS, _ := fs.Sub(ungitgoassets.FS, "components")
	mux.Handle("/plugins/", http.StripPrefix("/plugins/", http.FileServer(http.FS(componentFS))))

	publicFS, _ := fs.Sub(ungitgoassets.FS, "public")
	static := http.FileServer(http.FS(publicFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(s.indexHTML)
			return
		}
		static.ServeHTTP(w, r)
	})
	var h http.Handler = mux
	h = s.authMiddleware(h)
	h = noCache(h)
	h = allowedIPMiddleware(s.cfg.AllowedIPs, h)
	h = rootPathMiddleware(s.cfg.RootPath, h)
	// Correlation IDs and panic recovery are always installed. The
	// logRESTRequests option only controls routine lifecycle events.
	h = s.instrumentHTTP(h)
	return h
}

func (s *Server) SetCredentialEndpoint(baseURL string) {
	s.credentialMu.Lock()
	s.credentialEndpoint = strings.TrimRight(baseURL, "/")
	s.credentialMu.Unlock()
}

func (s *Server) credentialServerURL() string {
	s.credentialMu.RLock()
	defer s.credentialMu.RUnlock()
	if s.credentialEndpoint != "" {
		return s.credentialEndpoint
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", s.cfg.Port, s.cfg.RootPath)
}

func rootPathMiddleware(root string, next http.Handler) http.Handler {
	if root == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == root {
			http.Redirect(w, r, root+"/", http.StatusFound)
			return
		}
		if !strings.HasPrefix(r.URL.Path, root+"/") {
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = strings.TrimPrefix(r.URL.Path, root)
		if clone.URL.Path == "" {
			clone.URL.Path = "/"
		}
		next.ServeHTTP(w, clone)
	})
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}

func buildIndex(rootPath string) ([]byte, error) {
	data, err := fs.ReadFile(ungitgoassets.FS, "public/index.html")
	if err != nil {
		return nil, err
	}
	plugins, err := compileBuiltins(ungitgoassets.FS, rootPath)
	if err != nil {
		return nil, err
	}
	text := strings.Replace(string(data), "<!-- ungit-plugins-placeholder -->", plugins, 1)
	text = strings.ReplaceAll(text, "__ROOT_PATH__", rootPath)
	return []byte(text), nil
}

func packageVersion() (string, error) {
	data, err := fs.ReadFile(ungitgoassets.FS, "package.json")
	if err != nil {
		return "", err
	}
	var p struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return "", err
	}
	if p.Version == "" {
		return "", fmt.Errorf("package.json has no version")
	}
	return p.Version, nil
}

func (s *Server) serverData(w http.ResponseWriter, r *http.Request) {
	raw, err := json.Marshal(s.cfg)
	if err != nil {
		logAPIError(r.Context(), "api.server_data.encode_failed", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var visible map[string]any
	if err := json.Unmarshal(raw, &visible); err != nil {
		logAPIError(r.Context(), "api.server_data.decode_failed", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	home, _ := os.UserHomeDir()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	gitVersion := gitinfo.GetVersionInfo(ctx, s.cfg.GitBinPath).Version
	cancel()
	visible["homedir"] = home
	visible["gitVersion"] = gitVersion
	visible["ungitPackageVersion"] = s.version
	visible["ungitDevVersion"] = s.version
	visible["isGitOptionalLocks"] = gitVersion == "2.15.0"
	cfgJSON, err := json.Marshal(visible)
	if err != nil {
		logAPIError(r.Context(), "api.server_data.render_failed", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	platform := map[string]string{"windows": "win32", "darwin": "darwin", "linux": "linux"}[runtime.GOOS]
	if platform == "" {
		platform = runtime.GOOS
	}
	userHash := stableUserHash()
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	fmt.Fprintf(w, "ungit.config = %s;\n", cfgJSON)
	fmt.Fprintf(w, "ungit.userHash = %q;\n", userHash)
	fmt.Fprintf(w, "ungit.version = %q;\n", s.version)
	fmt.Fprintf(w, "ungit.platform = %q;\n", platform)
	fmt.Fprintf(w, "ungit.pluginApiVersion = %q;\n", pluginAPIVersion)
}

func stableUserHash() string {
	// upstream Ungit hashes the MAC address returned by getmac with MD5. Use the
	// first usable hardware address and preserve its fallback when none exists.
	mac := "abcde"
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if len(iface.HardwareAddr) == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			mac = iface.HardwareAddr.String()
			break
		}
	}
	sum := md5.Sum([]byte(mac))
	return hex.EncodeToString(sum[:])
}

func userConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ungitrc"
	}
	return filepath.Join(home, ".ungitrc")
}

func (s *Server) getUserConfig(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(userConfigPath())
	if os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	if err != nil {
		logAPIError(r.Context(), "api.user_config.read_failed", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		logAPIError(r.Context(), "api.user_config.decode_failed", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) postUserConfig(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var value any
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&value); err != nil {
		logAPIError(r.Context(), "api.user_config.request_decode_failed", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		err = os.WriteFile(userConfigPath(), data, 0o644)
	}
	if err != nil {
		logAPIError(r.Context(), "api.user_config.write_failed", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
