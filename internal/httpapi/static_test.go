package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestStaticAssetsServeTheCurrentBuild guards against shipping a binary whose
// embedded UI is out of sync with the Go code around it.
func TestStaticAssetsServeTheCurrentBuild(t *testing.T) {
	server, _ := newTestServer(t)

	res, err := http.Get(server.URL + "/static/app.js")
	if err != nil {
		t.Fatalf("fetch app.js: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("app.js status %d", res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected the embedded assets to be revalidated, got Cache-Control %q", cc)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(body)

	// Every one of these belongs to the push token work; if the UI is stale the
	// page silently falls back to the old single-token form.
	for _, marker := range []string{"'/push-tokens'", "tokenModal", "push_token_id", "token_masked", "令牌管理", "dataPanel", "wireDataPanel", "/backup/info"} {
		if !strings.Contains(js, marker) {
			t.Errorf("the embedded app.js is missing %q", marker)
		}
	}
}

// TestEditorAssetsAreServed checks that the editor pieces the pages need are
// actually in the binary, and that the page loads them. A missing script
// leaves the editor half working, which is hard to spot by hand.
func TestEditorAssetsAreServed(t *testing.T) {
	server, _ := newTestServer(t)

	for _, name := range []string{"editor.js", "strategy-editor.js", "device-test.js", "token-test.js", "logs.js", "data.js"} {
		res, err := http.Get(server.URL + "/static/" + name)
		if err != nil {
			t.Fatalf("fetch %s: %v", name, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Errorf("%s status %d", name, res.StatusCode)
			continue
		}
		if len(body) == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	// The page has to load every one of them, in an order that lets each one
	// use the helpers of the ones before it.
	res, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("fetch index: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	html := string(body)
	for _, name := range []string{"editor.js", "strategy-editor.js", "device-test.js", "token-test.js", "logs.js", "data.js", "app.js"} {
		if !strings.Contains(html, "/static/"+name) {
			t.Errorf("index.html does not load %s", name)
		}
	}
	// The log page is called by name, so the file has to be loaded.
	if !strings.Contains(html, "logs.js") {
		t.Error("index.html does not load logs.js")
	}
}
