package credentials

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunHelper(t *testing.T) {
	t.Setenv("UNGIT_GO_REQUEST_ID", "req-123456789abc")
	t.Setenv("UNGIT_GO_ACTION_ID", "action-123456789abc")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/credentials" {
			t.Fatalf("path=%q", got)
		}
		if got := r.URL.Query().Get("socketId"); got != "17" {
			t.Fatalf("socketId=%q", got)
		}
		if got := r.URL.Query().Get("remote"); got != "https://example/repo" {
			t.Fatalf("remote=%q", got)
		}
		if got := r.Header.Get("X-Request-ID"); got != "req-123456789abc" {
			t.Fatalf("X-Request-ID=%q", got)
		}
		if got := r.Header.Get("X-Action-ID"); got != "action-123456789abc" {
			t.Fatalf("X-Action-ID=%q", got)
		}
		_ = json.NewEncoder(w).Encode(Payload{Username: "alice", Password: "secret"})
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := RunHelper([]string{"17", server.URL, "https://example/repo", "get"}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "username=alice\npassword=secret\n"; got != want {
		t.Fatalf("output=%q want=%q", got, want)
	}
}
