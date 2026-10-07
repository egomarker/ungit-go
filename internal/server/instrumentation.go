package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egomarker/ungit-go/internal/observability"
)

const maxClientLogBodyBytes = 64 << 10

type serverMetrics struct {
	requests       atomic.Uint64
	requestErrors  atomic.Uint64
	activeRequests atomic.Int64
	clientErrors   atomic.Uint64
}

type responseObserver struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseObserver) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseObserver) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

func (w *responseObserver) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseObserver) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseObserver) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("HTTP hijacking is unsupported")
	}
	return hijacker.Hijack()
}

func (w *responseObserver) ReadFrom(reader io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		count, err := readerFrom.ReadFrom(reader)
		w.bytes += count
		return count, err
	}
	count, err := io.Copy(struct{ io.Writer }{w}, reader)
	return count, err
}

func (s *Server) instrumentHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := acceptedRequestID(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = observability.NewID("req")
		} else if acceptedRelatedID(requestID, "req") == "" && acceptedRelatedID(requestID, "ext") == "" {
			// Treat arbitrary caller-provided IDs as untrusted free text while retaining a
			// deterministic value that can correlate proxy and application logs. IDs
			// previously issued by this process are preserved for credential-helper calls.
			requestID = "ext-" + observability.Fingerprint(requestID)
		}
		actionID := acceptedRelatedID(r.Header.Get("X-Action-ID"), "action")
		if actionID == "" {
			actionID = observability.NewID("action")
		}
		ctx := observability.WithActionID(observability.WithRequestID(r.Context(), requestID), actionID)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Action-ID", actionID)
		observer := &responseObserver{ResponseWriter: w}
		active := s.metrics.activeRequests.Add(1)
		total := s.metrics.requests.Add(1)
		fields := requestFields(r)
		fields = append(fields,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_ip", remoteIP(r.RemoteAddr),
			"content_length", r.ContentLength,
			"active_requests", active,
			"request_count", total,
		)
		if s.cfg.LogRESTRequests {
			observability.Info(ctx, "http.request.started", "HTTP request started", fields...)
		}

		defer func() {
			remaining := s.metrics.activeRequests.Add(-1)
			if recovered := recover(); recovered != nil {
				if observer.status == 0 {
					http.Error(observer, "internal server error", http.StatusInternalServerError)
				}
				s.metrics.requestErrors.Add(1)
				observability.Error(ctx, "http.request.panic", "HTTP handler panicked", fmt.Errorf("%v", recovered),
					"method", r.Method,
					"path", r.URL.Path,
					"status", observer.status,
					"duration_ms", time.Since(started).Milliseconds(),
					"stack", string(debug.Stack()),
				)
				return
			}
			status := observer.status
			if status == 0 {
				status = http.StatusOK
			}
			levelEvent := "http.request.completed"
			message := "HTTP request completed"
			if status >= 400 {
				s.metrics.requestErrors.Add(1)
				levelEvent = "http.request.failed"
				message = "HTTP request failed"
			}
			completionFields := append(requestFields(r),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"response_bytes", observer.bytes,
				"duration_ms", time.Since(started).Milliseconds(),
				"active_requests", remaining,
			)
			if s.cfg.LogRESTRequests {
				if status >= 400 {
					observability.Warn(ctx, levelEvent, message, completionFields...)
				} else {
					observability.Info(ctx, levelEvent, message, completionFields...)
				}
			}
		}()

		next.ServeHTTP(observer, r)
	})
}

func acceptedRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.:", char) {
			continue
		}
		return ""
	}
	return value
}

func acceptedRelatedID(value, prefix string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, prefix+"-") {
		return ""
	}
	suffix := strings.TrimPrefix(value, prefix+"-")
	if len(suffix) != 12 && len(suffix) != 16 {
		return ""
	}
	for _, char := range suffix {
		if (char >= 'a' && char <= 'f') || (char >= '0' && char <= '9') {
			continue
		}
		return ""
	}
	return value
}

func acceptedSocketID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 20 {
		return ""
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return ""
		}
	}
	return value
}

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func requestFields(r *http.Request) []any {
	query := r.URL.Query()
	fields := []any{}
	if value := query.Get("path"); value != "" {
		fields = append(fields, "repository", value)
	}
	if value := query.Get("remote"); value != "" {
		fields = append(fields, "remote", observability.RedactURL(value))
	}
	for _, key := range []string{"ref", "sha1", "name", "file", "filename", "version"} {
		if value := query.Get(key); value != "" {
			fields = append(fields, key+"_summary", observability.RedactFreeText(value))
		}
	}
	if r.URL.RawQuery != "" {
		fields = append(fields, "query_parameter_count", len(query))
	}
	return fields
}

type clientLogPayload struct {
	Level     string         `json:"level"`
	Event     string         `json:"event"`
	Message   string         `json:"message"`
	Stack     string         `json:"stack"`
	Page      string         `json:"page"`
	SocketID  string         `json:"socketId"`
	Details   map[string]any `json:"details"`
	Timestamp string         `json:"timestamp"`
}

func (s *Server) postClientLog(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxClientLogBodyBytes)
	defer body.Close()
	var payload clientLogPayload
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&payload); err != nil {
		observability.Warn(r.Context(), "client.log.rejected", "browser diagnostic payload rejected", observability.ErrorFields(err)...)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid client log payload"})
		return
	}
	if strings.TrimSpace(payload.Event) == "" {
		payload.Event = "browser.message"
	}
	count := s.metrics.clientErrors.Add(1)
	ctx := r.Context()
	if socketID := acceptedSocketID(payload.SocketID); socketID != "" && s.realtime.get(socketID) != nil {
		ctx = observability.WithSocketID(ctx, socketID)
	}
	clientEvent := acceptedClientEvent(payload.Event)
	fields := []any{
		"client_event", clientEvent,
		"client_message", observability.RedactFreeText(payload.Message),
		"client_stack", observability.RedactFreeText(payload.Stack),
		"page_summary", observability.RedactFreeText(payload.Page),
		"client_timestamp", acceptedClientTimestamp(payload.Timestamp),
		"client_error_count", count,
		"user_agent_summary", observability.RedactFreeText(r.UserAgent()),
	}
	if len(payload.Details) > 0 {
		fields = append(fields, "details", sanitizeClientDetails(payload.Details))
		if relatedRequestID, ok := payload.Details["requestId"].(string); ok {
			if relatedRequestID = acceptedRelatedID(relatedRequestID, "req"); relatedRequestID == "" {
				relatedRequestID = acceptedRelatedID(payload.Details["requestId"].(string), "ext")
			}
			if relatedRequestID != "" {
				fields = append(fields, "related_request_id", relatedRequestID)
			}
		}
		if relatedActionID, ok := payload.Details["actionId"].(string); ok {
			if relatedActionID = acceptedRelatedID(relatedActionID, "action"); relatedActionID != "" {
				fields = append(fields, "related_action_id", relatedActionID)
			}
		}
	}
	switch strings.ToLower(payload.Level) {
	case "debug":
		observability.Debug(ctx, "client."+clientEvent, "browser diagnostic", fields...)
	case "info":
		observability.Info(ctx, "client."+clientEvent, "browser diagnostic", fields...)
	case "warn", "warning":
		observability.Warn(ctx, "client."+clientEvent, "browser diagnostic", fields...)
	default:
		observability.Error(ctx, "client."+clientEvent, "browser error", nil, fields...)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{})
}

func acceptedClientEvent(value string) string {
	value = strings.TrimSpace(value)
	allowed := map[string]bool{
		"api.request_failed":          true,
		"api.unhandled_rejection":     true,
		"browser.message":             true,
		"browser.unhandled_rejection": true,
		"browser.window_error":        true,
		"realtime.connect_error":      true,
		"realtime.disconnect":         true,
	}
	if allowed[value] {
		return value
	}
	return "browser.message"
}

func acceptedClientTimestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return "<invalid>"
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func sanitizeClientDetails(input map[string]any) map[string]any {
	allowed := map[string]bool{
		"actionId": true, "column": true, "errorCode": true, "filename": true, "line": true,
		"method": true, "path": true, "requestId": true, "status": true,
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		if !allowed[key] {
			continue
		}
		switch typed := value.(type) {
		case string:
			switch key {
			case "requestId":
				if accepted := acceptedRelatedID(typed, "req"); accepted != "" {
					output[key] = accepted
				} else if accepted := acceptedRelatedID(typed, "ext"); accepted != "" {
					output[key] = accepted
				}
			case "actionId":
				if accepted := acceptedRelatedID(typed, "action"); accepted != "" {
					output[key] = accepted
				}
			case "method":
				method := strings.ToUpper(strings.TrimSpace(typed))
				if method == "GET" || method == "POST" || method == "PUT" || method == "DELETE" {
					output[key] = method
				}
			case "path":
				output["pathSummary"] = observability.RedactFreeText(typed)
			case "errorCode":
				output[key] = acceptedClientCode(typed)
			case "filename":
				output[key] = observability.RedactFreeText(typed)
			}
		case float64:
			if key == "column" || key == "line" || key == "status" {
				output[key] = typed
			}
		case bool, nil:
			output[key] = typed
		}
	}
	return output
}

func acceptedClientCode(value string) string {
	value = strings.TrimSpace(value)
	allowed := map[string]bool{
		"authentication-failed": true, "authentication-required": true,
		"credential-request-cancelled": true, "cross-domain-error": true,
		"error-appending-ignore": true, "invalid-socket-id": true,
		"local-changes-would-be-overwritten": true, "merge-failed": true,
		"missing-request-parameter": true, "must-be-in-working-tree": true,
		"no-commits": true, "no-git-name-email-configured": true, "no-head": true,
		"no-remote-configured": true, "no-remote-specified": true, "no-such-file": true,
		"no-such-path": true, "no-supported-authentication-provided": true,
		"non-fast-forward": true, "not-a-repository": true, "offline": true,
		"permision-denied-publickey": true, "proxy-authentication-required": true,
		"remote-timeout": true, "request-from-unathorized-location": true,
		"socket-unavailable": true, "ssh-bad-file-number": true, "timeout": true,
		"unknown": true,
	}
	if allowed[value] {
		return value
	}
	return "unknown"
}

func logAPIError(ctx context.Context, event string, err error, fields ...any) {
	if err == nil {
		return
	}
	fields = append(fields, observability.ErrorFields(err)...)
	observability.Error(ctx, event, "API operation failed", nil, fields...)
}
