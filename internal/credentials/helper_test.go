package credentials

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunHelper(t *testing.T) {
	t.Parallel()
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
