package credentials

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Payload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// RunHelper implements the Git credential-helper protocol used by Ungit-Go. Args
// are: socket ID, server base URL, remote name/url, and the Git helper action.
func RunHelper(args []string, out io.Writer) error {
	if len(args) < 4 {
		return fmt.Errorf("credential-helper: expected socketId, serverURL, remote and action")
	}
	socketID, serverURL, remote, action := args[0], strings.TrimRight(args[1], "/"), args[2], args[3]
	if action != "get" {
		// Node Ungit-compatible helper only handles get; store/erase are intentionally no-op.
		return nil
	}

	u, err := url.Parse(serverURL + "/api/credentials")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("socketId", socketID)
	q.Set("remote", remote)
	u.RawQuery = q.Encode()

	request, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("credential-helper request: %w", err)
	}
	if requestID := os.Getenv("UNGIT_GO_REQUEST_ID"); requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}
	if actionID := os.Getenv("UNGIT_GO_ACTION_ID"); actionID != "" {
		request.Header.Set("X-Action-ID", actionID)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("credential-helper query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("credential-helper server returned %s", resp.Status)
	}
	var payload Payload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("credential-helper response: %w", err)
	}
	fmt.Fprintf(out, "username=%s\n", payload.Username)
	fmt.Fprintf(out, "password=%s\n", payload.Password)
	return nil
}
