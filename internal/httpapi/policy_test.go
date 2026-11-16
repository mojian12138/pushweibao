package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// TestThePolicyAllowsTheStyleOfAMessage checks that the policy of the pages
// lets the style of a message through.
//
// A message is written with style attributes, and the phone draws them. The
// preview shows the message itself, so a policy that forbids inline styles
// would show the message without its frame and colours — the very thing the
// preview is there to show.
func TestThePolicyAllowsTheStyleOfAMessage(t *testing.T) {
	server, _ := newTestServer(t)
	res, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	policy := res.Header.Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("the pages have no policy")
	}
	if !policyAllows(policy, "style-src", "'unsafe-inline'") {
		t.Errorf("the policy forbids the style of a message: %q", policy)
	}
	// The scripts stay limited to the files of the program, because they are
	// the code that could run on the page.
	if policyAllows(policy, "script-src", "'unsafe-inline'") {
		t.Errorf("the policy lets a script run from the page: %q", policy)
	}
}

// policyAllows reports whether a policy has a value under a directive.
func policyAllows(policy, directive, value string) bool {
	for _, part := range strings.Split(policy, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 || fields[0] != directive {
			continue
		}
		for _, f := range fields[1:] {
			if f == value {
				return true
			}
		}
	}
	return false
}
