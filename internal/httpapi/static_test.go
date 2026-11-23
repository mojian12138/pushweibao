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

// TestDownloadReadsTheResponseBody guards the download helper. It used to go
// through api(), which turns a json answer into an object, so saving the
// backup file failed with "Failed to execute 'createObjectURL' on 'URL'".
// A download has to take the bytes.
func TestDownloadReadsTheResponseBody(t *testing.T) {
	server, _ := newTestServer(t)
	res, err := http.Get(server.URL + "/static/app.js")
	if err != nil {
		t.Fatalf("fetch app.js: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(body)
	start := strings.Index(js, "async function download(")
	if start < 0 {
		t.Fatal("app.js has no download helper")
	}
	end := strings.Index(js[start:], "\n}")
	if end < 0 {
		t.Fatal("the download helper has no end")
	}
	helper := js[start : start+end]
	if !strings.Contains(helper, "res.blob()") {
		t.Error("download() does not read the response as a blob")
	}
	if strings.Contains(helper, "await api(") {
		t.Error("download() goes through api(), which parses a json answer instead of saving it")
	}
	if !strings.Contains(helper, "Content-Disposition") {
		t.Error("download() ignores the file name the server sends")
	}
}

// TestBackupAnswersWithADownloadName checks the header the page reads, so the
// saved file keeps its date instead of a path fragment.
func TestBackupAnswersWithADownloadName(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, _ := login(t, server, "admin", "SuperSecret12345")
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/backup", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	disposition := res.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, "filename*=UTF-8''") {
		t.Errorf("the backup sends no escaped file name: %q", disposition)
	}
	if !strings.Contains(disposition, "维保备份") && !strings.Contains(disposition, "%E7%BB%B4%E4%BF%9D%E5%A4%87%E4%BB%BD") {
		t.Errorf("the backup file name is not the one the page shows: %q", disposition)
	}
}
