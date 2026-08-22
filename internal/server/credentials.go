package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (s *Server) getCredentials(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"errorCode": "request-from-unathorized-location"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	payload, err := s.realtime.requestCredentials(ctx, r.URL.Query().Get("socketId"), r.URL.Query().Get("remote"))
	if err != nil {
		code := "socket-unavailable"
		if ctx.Err() != nil {
			code = "credential-request-cancelled"
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"errorCode": code})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func socketIDString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case jsonNumberStringer:
		return v.String()
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

type jsonNumberStringer interface{ String() string }

func (s *Server) requireSocket(w http.ResponseWriter, value any) (string, bool) {
	id := socketIDString(value)
	if id == "ignore" {
		return id, true
	}
	if id == "" || s.realtime.get(id) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No such socket: " + id, "errorCode": "invalid-socket-id"})
		return id, false
	}
	return id, true
}

func (s *Server) credentialArgs(socketID any, remote string) []string {
	id := socketIDString(socketID)
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		// upstream Ungit normalizes the helper path to forward slashes so Git for
		// Windows' sh-compatible credential.helper command can execute it.
		executable = strings.ReplaceAll(executable, `\`, "/")
	}
	// Git treats a helper beginning with ! as a shell snippet and appends the
	// helper action (get/store/erase). The same Ungit-Go binary handles that mode.
	command := shellQuote(executable) + " credential-helper " +
		shellQuote(id) + " " + shellQuote(s.credentialServerURL()) + " " + shellQuote(remote)
	return []string{"-c", "credential.helper=!" + command}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
