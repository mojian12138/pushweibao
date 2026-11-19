package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestImportRejectsBodyAboveConfiguredLimit(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "oversize.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte(strings.Repeat("x", 33<<20))); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/devices/import", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload status=%d, want 413", res.StatusCode)
	}
}
