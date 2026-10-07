package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/egomarker/ungit-go/internal/observability"
)

const sessionCookieName = "connect.sid"

type authSession struct {
	username string
	expires  time.Time
}

type authManager struct {
	enabled bool
	users   map[string]string
	mu      sync.Mutex
	session map[string]authSession
}

func newAuthManager(enabled bool, users map[string]string) *authManager {
	copied := make(map[string]string, len(users))
	for k, v := range users {
		copied[k] = v
	}
	return &authManager{enabled: enabled, users: copied, session: map[string]authSession{}}
}

func (a *authManager) authenticated(r *http.Request) bool {
	if !a.enabled {
		return true
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	now := time.Now()
	a.mu.Lock()
	sess, ok := a.session[cookie.Value]
	if ok && now.After(sess.expires) {
		delete(a.session, cookie.Value)
		ok = false
	}
	if ok {
		// express-session with resave/touch semantics keeps active sessions alive.
		sess.expires = now.Add(24 * time.Hour)
		a.session[cookie.Value] = sess
	}
	a.mu.Unlock()
	return ok
}

func (a *authManager) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		observability.Warn(r.Context(), "auth.login.decode_failed", "failed to decode login request", observability.ErrorFields(err)...)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if want, ok := a.users[body.Username]; !ok || body.Password != want {
		observability.Warn(r.Context(), "auth.login.failed", "authentication failed", "remote_ip", remoteIP(r.RemoteAddr))
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"errorCode": "authentication-failed",
			"error":     "No such username/password",
		})
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		observability.Error(r.Context(), "auth.login.session_failed", "failed to create authentication session", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	token := hex.EncodeToString(buf)
	a.mu.Lock()
	a.session[token] = authSession{username: body.Username, expires: time.Now().Add(24 * time.Hour)}
	a.mu.Unlock()
	observability.Info(r.Context(), "auth.login.completed", "authentication succeeded", "remote_ip", remoteIP(r.RemoteAddr))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *authManager) loggedIn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"loggedIn": a.authenticated(r)})
}

func (a *authManager) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		a.mu.Lock()
		delete(a.session, cookie.Value)
		a.mu.Unlock()
	}
	observability.Info(r.Context(), "auth.logout.completed", "authentication session ended", "remote_ip", remoteIP(r.RemoteAddr))
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	if !s.auth.enabled {
		return next
	}
	public := map[string]bool{
		"/api/login":         true,
		"/api/logout":        true,
		"/api/loggedin":      true,
		"/api/ping":          true,
		"/api/gitversion":    true,
		"/api/latestversion": true,
		"/api/credentials":   true,
		"/api/client-log":    true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || public[r.URL.Path] || s.auth.authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		observability.Warn(r.Context(), "auth.request.rejected", "unauthenticated API request rejected",
			"method", r.Method, "path", r.URL.Path, "remote_ip", remoteIP(r.RemoteAddr))
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"errorCode": "authentication-required",
			"error":     "You have to authenticate to access this resource",
		})
	})
}

func allowedIPMiddleware(allowed []string, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	set := make(map[string]bool, len(allowed))
	for _, ip := range allowed {
		set[ip] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if set[host] || set[r.RemoteAddr] {
			next.ServeHTTP(w, r)
			return
		}
		observability.Warn(r.Context(), "network.ip.rejected", "request rejected by allowed IP policy",
			"method", r.Method, "path", r.URL.Path, "remote_ip", host)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<h3>This host is not authorized to connect</h3><p>You are trying to connect to an Ungit-Go instance from an unauthorized host.</p>"))
	})
}
