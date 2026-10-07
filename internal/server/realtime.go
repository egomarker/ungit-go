package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egomarker/ungit-go/internal/observability"
)

type realtimeEvent struct {
	Name string
	Data any
}

type credentialPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type realtimeClient struct {
	id        string
	ctx       context.Context
	events    chan realtimeEvent
	mu        sync.Mutex
	watchStop context.CancelFunc
	watchPath string
	credWait  []chan credentialPayload
}

type realtimeHub struct {
	server  *Server
	mu      sync.Mutex
	nextID  int
	clients map[string]*realtimeClient
}

func newRealtimeHub(s *Server) *realtimeHub {
	return &realtimeHub{server: s, clients: map[string]*realtimeClient{}}
}

func (h *realtimeHub) newClient(ctx context.Context) *realtimeClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := strconv.Itoa(h.nextID)
	h.nextID++
	ctx = observability.WithSocketID(ctx, id)
	c := &realtimeClient{id: id, ctx: ctx, events: make(chan realtimeEvent, 64)}
	h.clients[id] = c
	observability.Info(ctx, "realtime.client.created", "realtime client created", "active_clients", len(h.clients))
	return c
}

func (h *realtimeHub) get(id string) *realtimeClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clients[id]
}

func (h *realtimeHub) remove(id string) {
	h.mu.Lock()
	c := h.clients[id]
	delete(h.clients, id)
	remaining := len(h.clients)
	h.mu.Unlock()
	if c != nil {
		c.mu.Lock()
		if c.watchStop != nil {
			c.watchStop()
		}
		for _, waiter := range c.credWait {
			close(waiter)
		}
		credentialWaiters := len(c.credWait)
		c.credWait = nil
		watchPath := c.watchPath
		c.mu.Unlock()
		observability.Info(c.ctx, "realtime.client.removed", "realtime client removed",
			"active_clients", remaining,
			"watch_path", watchPath,
			"cancelled_credential_waiters", credentialWaiters,
		)
	}
}

func (h *realtimeHub) emit(c *realtimeClient, name string, data any) {
	select {
	case c.events <- realtimeEvent{Name: name, Data: data}:
		observability.Debug(c.ctx, "realtime.event.queued", "realtime event queued",
			"realtime_event", name, "queue_depth", len(c.events))
	default:
		// Keep state notifications lossy instead of letting a stalled browser block Git operations.
		observability.Warn(c.ctx, "realtime.event.dropped", "realtime event dropped because the client queue is full",
			"realtime_event", name, "queue_depth", len(c.events))
	}
}

func (h *realtimeHub) broadcast(repoPath, name string, data any) {
	want := filepath.Clean(repoPath)
	h.mu.Lock()
	clients := make([]*realtimeClient, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.mu.Lock()
		watchPath := c.watchPath
		c.mu.Unlock()
		if watchPath != "" && filepath.Clean(watchPath) == want {
			h.emit(c, name, data)
		}
	}
}

func (h *realtimeHub) handleConnect(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		observability.Warn(r.Context(), "realtime.connect.rejected", "realtime streaming is unsupported")
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	c := h.newClient(r.Context())
	started := time.Now()
	defer func() {
		observability.Info(c.ctx, "realtime.connect.closed", "realtime connection closed",
			"duration_ms", time.Since(started).Milliseconds(), "reason", r.Context().Err())
		h.remove(c.id)
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if err := writeSSE(w, realtimeEvent{Name: "connected", Data: map[string]string{"socketId": c.id}}); err != nil {
		observability.Error(c.ctx, "realtime.connect.write_failed", "failed to write realtime connection event", err)
		return
	}
	flusher.Flush()
	observability.Info(c.ctx, "realtime.connect.opened", "realtime connection opened")

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-c.events:
			if err := writeSSE(w, ev); err != nil {
				observability.Error(c.ctx, "realtime.event.write_failed", "failed to write realtime event", err,
					"realtime_event", ev.Name)
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				observability.Error(c.ctx, "realtime.heartbeat.write_failed", "failed to write realtime heartbeat", err)
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev realtimeEvent) error {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, data)
	return err
}

func (h *realtimeHub) handleEmit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("socketId")
	c := h.get(id)
	if c == nil {
		observability.Warn(r.Context(), "realtime.emit.rejected", "realtime event rejected for unknown socket", "requested_socket_id", safeSocketID(id))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No such socket: " + id, "errorCode": "invalid-socket-id"})
		return
	}
	var body struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		observability.Warn(r.Context(), "realtime.emit.decode_failed", "failed to decode realtime event", observability.ErrorFields(err)...)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx := observability.WithSocketID(r.Context(), id)
	safeEvent := body.Event
	if safeEvent != "watch" && safeEvent != "credentials" {
		safeEvent = "unknown"
	}
	observability.Info(ctx, "realtime.emit.received", "realtime event received", "realtime_event", safeEvent, "payload_bytes", len(body.Data))
	switch body.Event {
	case "watch":
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(body.Data, &payload); err != nil || strings.TrimSpace(payload.Path) == "" {
			fields := []any{"realtime_event", safeEvent}
			if err != nil {
				fields = append(fields, observability.ErrorFields(err)...)
			}
			observability.Warn(ctx, "realtime.emit.invalid", "invalid realtime watch event", fields...)
			writeJSON(w, http.StatusBadRequest, map[string]string{"errorCode": "missing-request-parameter", "error": "watch requires path"})
			return
		}
		h.startWatch(ctx, c, filepath.Clean(payload.Path))
	case "credentials":
		var payload credentialPayload
		if err := json.Unmarshal(body.Data, &payload); err != nil {
			observability.Warn(ctx, "realtime.emit.invalid", "invalid realtime credentials event",
				append([]any{"realtime_event", safeEvent}, observability.ErrorFields(err)...)...)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		c.mu.Lock()
		if len(c.credWait) > 0 {
			waiter := c.credWait[0]
			c.credWait = c.credWait[1:]
			select {
			case waiter <- payload:
			default:
			}
		}
		c.mu.Unlock()
	default:
		observability.Warn(ctx, "realtime.emit.unknown", "unknown realtime event ignored")
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *realtimeHub) startWatch(actionCtx context.Context, c *realtimeClient, repoPath string) {
	c.mu.Lock()
	if c.watchStop != nil {
		c.watchStop()
	}
	watcherID := observability.NewID("watch")
	baseCtx := context.WithoutCancel(actionCtx)
	ctx, cancel := context.WithCancel(observability.WithWatcherID(baseCtx, watcherID))
	c.watchStop = cancel
	c.watchPath = repoPath
	c.mu.Unlock()
	ready := make(chan struct{})
	go h.server.watchRepository(ctx, c, repoPath, ready)
	select {
	case <-ready:
		observability.Info(ctx, "watch.ready", "repository watcher is ready", "repository", repoPath)
	case <-time.After(2 * time.Second):
		observability.Warn(ctx, "watch.ready_timeout", "repository watcher did not report readiness within two seconds", "repository", repoPath)
	}
}

func (h *realtimeHub) requestCredentials(ctx context.Context, socketID, remote string) (credentialPayload, error) {
	if safeID := acceptedSocketID(socketID); safeID != "" {
		ctx = observability.WithSocketID(ctx, safeID)
	}
	c := h.get(socketID)
	if c == nil {
		err := fmt.Errorf("socket-unavailable")
		observability.Error(ctx, "credentials.request.failed", "credential request has no active socket", err,
			"remote", observability.RedactURL(remote))
		return credentialPayload{}, err
	}
	waiter := make(chan credentialPayload, 1)
	c.mu.Lock()
	c.credWait = append(c.credWait, waiter)
	c.mu.Unlock()

	started := time.Now()
	observability.Info(ctx, "credentials.request.started", "credential request started", "remote", observability.RedactURL(remote))
	h.emit(c, "request-credentials", map[string]string{"remote": remote})
	select {
	case <-ctx.Done():
		observability.Error(ctx, "credentials.request.cancelled", "credential request cancelled", ctx.Err(),
			"remote", observability.RedactURL(remote), "duration_ms", time.Since(started).Milliseconds())
		return credentialPayload{}, ctx.Err()
	case payload, ok := <-waiter:
		if !ok {
			err := fmt.Errorf("socket-unavailable")
			observability.Error(ctx, "credentials.request.failed", "credential request socket closed", err,
				"remote", observability.RedactURL(remote), "duration_ms", time.Since(started).Milliseconds())
			return credentialPayload{}, err
		}
		observability.Info(ctx, "credentials.request.completed", "credential request completed",
			"remote", observability.RedactURL(remote), "duration_ms", time.Since(started).Milliseconds(),
			"username_present", payload.Username != "", "password_present", payload.Password != "")
		return payload, nil
	}
}

func socketIOCompatClient(rootPath string) string {
	rootJSON, _ := json.Marshal(rootPath)
	return fmt.Sprintf(`(function (global) {
  var rootPath = %s;
  global.io = function () {
    var handlers = {};
    var socketId = null;
    var source = null;
    var socket = {
      on: function (name, fn) {
        (handlers[name] = handlers[name] || []).push(fn);
        return socket;
      },
      emit: function (name, data, callback) {
        if (socketId === null) return socket;
        fetch(rootPath + '/realtime/emit?socketId=' + encodeURIComponent(socketId), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ event: name, data: data || {} })
        }).then(function (response) {
          if (!response.ok) throw new Error('realtime emit failed: ' + response.status);
          if (typeof callback === 'function') callback();
        }).catch(function (err) {
          dispatch('connect_error', err);
        });
        return socket;
      }
    };
    function dispatch(name, data) {
      (handlers[name] || []).slice().forEach(function (fn) { fn(data); });
    }
    function connect() {
      source = new EventSource(rootPath + '/realtime/connect');
      ['connected', 'working-tree-changed', 'git-directory-changed', 'request-credentials'].forEach(function (name) {
        source.addEventListener(name, function (event) {
          var data = {};
          try { data = JSON.parse(event.data || '{}'); } catch (_) {}
          if (name === 'connected') socketId = data.socketId;
          dispatch(name, data);
        });
      });
      source.onerror = function (err) {
        dispatch('disconnect', err);
        dispatch('connect_error', err);
      };
    }
    setTimeout(connect, 0);
    return socket;
  };
})(window);
`, string(rootJSON))
}
