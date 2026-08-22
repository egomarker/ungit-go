package server

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

var addressParityCases = []struct {
	remote string
	want   map[string]string
}{
	{"ssh://some.address.com/my/awesome/project", map[string]string{"host": "some.address.com", "project": "my/awesome/project", "shortProject": "project"}},
	{"ssh://some.address.com:8080/my/awesome/project", map[string]string{"host": "some.address.com", "port": "8080", "project": "my/awesome/project", "shortProject": "project"}},
	{"some.address.com:my/awesome/project.git", map[string]string{"host": "some.address.com", "project": "my/awesome/project", "shortProject": "project"}},
	{"someuser@some.address.com:my/awesome/project.git", map[string]string{"username": "someuser", "host": "some.address.com", "project": "my/awesome/project", "shortProject": "project"}},
	{"https://some.address.com/my/awesome/project.git", map[string]string{"host": "some.address.com", "project": "my/awesome/project", "shortProject": "project"}},
	{"/home/username/somerepo", map[string]string{"host": "localhost", "project": "somerepo", "shortProject": "somerepo"}},
	{"~/something/somerepo", map[string]string{"host": "localhost", "project": "somerepo", "shortProject": "somerepo"}},
	{`C:\something\somerepo`, map[string]string{"host": "localhost", "project": "somerepo", "shortProject": "somerepo"}},
	{`C:\somerepo`, map[string]string{"host": "localhost", "project": "somerepo", "shortProject": "somerepo"}},
	{`C:\something\somerepo\`, map[string]string{"host": "localhost", "project": "somerepo", "shortProject": "somerepo"}},
}

func TestAddressParserParity(t *testing.T) {
	for _, tc := range addressParityCases {
		t.Run(tc.remote, func(t *testing.T) {
			got := parseAddress(tc.remote)
			for k, want := range tc.want {
				if got[k] != want {
					t.Fatalf("%s: %s=%q want %q; all=%v", tc.remote, k, got[k], want, got)
				}
			}
		})
	}
}

func TestAddressParserMatchesReferenceNodeImplementation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; static parity cases still run")
	}
	jsPath, err := filepath.Abs("../../public/source/address-parser.js")
	if err != nil {
		t.Fatal(err)
	}
	remotes := make([]string, len(addressParityCases))
	for i, tc := range addressParityCases {
		remotes[i] = tc.remote
	}
	input, _ := json.Marshal(remotes)
	script := `const fs=require('fs'); const p=require(process.argv[1]); const xs=JSON.parse(fs.readFileSync(0,'utf8')); process.stdout.write(JSON.stringify(xs.map(x=>p.parseAddress(x))));`
	cmd := exec.Command(node, "-e", script, jsPath)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("reference node parser: %v: %s", err, out)
	}
	var reference []map[string]string
	if err := json.Unmarshal(out, &reference); err != nil {
		t.Fatalf("decode reference output: %v: %s", err, out)
	}
	if len(reference) != len(remotes) {
		t.Fatalf("reference count=%d want %d", len(reference), len(remotes))
	}
	for i, remote := range remotes {
		got := parseAddress(remote)
		if !reflect.DeepEqual(got, reference[i]) {
			t.Fatalf("%q\nGo:   %#v\nNode: %#v", remote, got, reference[i])
		}
	}
}
