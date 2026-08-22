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

func (h *realtimeHub) newClient() *realtimeClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := strconv.Itoa(h.nextID)
	h.nextID++
	c := &realtimeClient{id: id, events: make(chan realtimeEvent, 64)}
	h.clients[id] = c
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
	h.mu.Unlock()
	if c != nil {
		c.mu.Lock()
		if c.watchStop != nil {
			c.watchStop()
		}
		for _, waiter := range c.credWait {
			close(waiter)
		}
		c.credWait = nil
		c.mu.Unlock()
	}
}

func (h *realtimeHub) emit(c *realtimeClient, name string, data any) {
	select {
	case c.events <- realtimeEvent{Name: name, Data: data}:
	default:
		// Keep state notifications lossy instead of letting a stalled browser block Git operations.
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
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	c := h.newClient()
	defer h.remove(c.id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	writeSSE(w, realtimeEvent{Name: "connected", Data: map[string]string{"socketId": c.id}})
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-c.events:
			writeSSE(w, ev)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev realtimeEvent) {
	data, _ := json.Marshal(ev.Data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, data)
}

func (h *realtimeHub) handleEmit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("socketId")
	c := h.get(id)
	if c == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No such socket: " + id, "errorCode": "invalid-socket-id"})
		return
	}
	var body struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	switch body.Event {
	case "watch":
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(body.Data, &payload); err != nil || strings.TrimSpace(payload.Path) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"errorCode": "missing-request-parameter", "error": "watch requires path"})
			return
		}
		h.startWatch(c, filepath.Clean(payload.Path))
	case "credentials":
		var payload credentialPayload
		if err := json.Unmarshal(body.Data, &payload); err != nil {
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
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *realtimeHub) startWatch(c *realtimeClient, repoPath string) {
	c.mu.Lock()
	if c.watchStop != nil {
		c.watchStop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.watchStop = cancel
	c.watchPath = repoPath
	c.mu.Unlock()
	ready := make(chan struct{})
	go h.server.watchRepository(ctx, c, repoPath, ready)
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
	}
}

func (h *realtimeHub) requestCredentials(ctx context.Context, socketID, remote string) (credentialPayload, error) {
	c := h.get(socketID)
	if c == nil {
		return credentialPayload{}, fmt.Errorf("socket-unavailable")
	}
	waiter := make(chan credentialPayload, 1)
	c.mu.Lock()
	c.credWait = append(c.credWait, waiter)
	c.mu.Unlock()

	h.emit(c, "request-credentials", map[string]string{"remote": remote})
	select {
	case <-ctx.Done():
		return credentialPayload{}, ctx.Err()
	case payload, ok := <-waiter:
		if !ok {
			return credentialPayload{}, fmt.Errorf("socket-unavailable")
		}
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
