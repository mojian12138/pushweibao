package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"maintenance-reminder/internal/auth"
)

// fetchBackup downloads the backup and returns the decoded document.
func fetchBackup(t *testing.T, server *httptest.Server, cookie *http.Cookie, csrf string) (backupDoc, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("backup status %d: %s", res.StatusCode, raw)
	}
	var doc backupDoc
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("backup is not valid json: %v", err)
	}
	return doc, raw
}

type backupDoc struct {
	Meta struct {
		Format    string         `json:"format"`
		Version   int            `json:"version"`
		CreatedAt string         `json:"created_at"`
		CreatedBy string         `json:"created_by"`
		Tables    []string       `json:"tables"`
		Counts    map[string]int `json:"counts"`
	} `json:"meta"`
	Data map[string][]map[string]any `json:"data"`
}

// postBackup uploads a backup file and returns the status and the envelope.
func postBackup(t *testing.T, server *httptest.Server, cookie *http.Cookie, csrf string, name string, content []byte, mode string) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(content); err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		if err = writer.WriteField("mode", mode); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/backup/restore", &body)
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
	out := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// TestBackupRoundTripDeletesAndRestores is the promise of the feature: a file
// downloaded now puts the data back exactly as it was after it is lost.
func TestBackupRoundTripDeletesAndRestores(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	projectID, err := database.Exec(`INSERT INTO projects(name,customer,contact,owner) VALUES('备份项目','客户甲','张三','李四')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := projectID.LastInsertId()
	if _, err = database.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status,owner) VALUES(?,?,?,?,?,?,?,?)`,
		id, "Acme", "XC-200", "SN-BACKUP-1", "2026-01-01", "2026-12-31", "active", "李四"); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO settings(key,value) VALUES('push.channel','webhook')`); err != nil {
		t.Fatal(err)
	}

	doc, raw := fetchBackup(t, server, cookie, csrf)
	if doc.Meta.Format != backupFormat {
		t.Fatalf("meta format = %q", doc.Meta.Format)
	}
	if doc.Meta.CreatedBy != "admin" {
		t.Errorf("meta created_by = %q, want admin", doc.Meta.CreatedBy)
	}
	if doc.Meta.Counts["devices"] < 1 || doc.Meta.Counts["projects"] < 1 {
		t.Errorf("meta counts = %v, want the inserted rows", doc.Meta.Counts)
	}
	if len(doc.Data["devices"]) != doc.Meta.Counts["devices"] {
		t.Errorf("device rows = %d, meta says %d", len(doc.Data["devices"]), doc.Meta.Counts["devices"])
	}

	// Lose the data the way an accident would.
	if _, err = database.Exec(`DELETE FROM devices`); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`DELETE FROM projects`); err != nil {
		t.Fatal(err)
	}

	status, out := postBackup(t, server, cookie, csrf, "backup.json", raw, "replace")
	if status != http.StatusOK {
		t.Fatalf("restore status %d: %v", status, out)
	}
	var serial string
	if err = database.QueryRow(`SELECT serial_number FROM devices WHERE serial_number='SN-BACKUP-1'`).Scan(&serial); err != nil {
		t.Fatalf("the device did not come back: %v", err)
	}
	var customer string
	if err = database.QueryRow(`SELECT customer FROM projects WHERE name='备份项目'`).Scan(&customer); err != nil {
		t.Fatalf("the project did not come back: %v", err)
	}
	if customer != "客户甲" {
		t.Errorf("customer = %q, want 客户甲", customer)
	}
}

// TestBackupRestoreKeepsTheCurrentAccount makes sure the person who runs a
// restore is not locked out by the file they picked.
func TestBackupRestoreKeepsTheCurrentAccount(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	// A backup from a time when the administrator had another password and
	// another account existed.
	otherHash := "$2a$10$abcdefghijklmnopqrstuv"
	file := `{"meta":{"format":"` + backupFormat + `","version":1,"created_at":"2026-01-01T00:00:00+08:00","created_by":"boss","tables":[],"counts":{}},` +
		`"data":{"users":[{"id":1,"username":"admin","password_hash":"` + otherHash + `","role":"viewer","active":1,"must_change_password":0,"pushplus_token":"","push_token_id":null},` +
		`{"id":2,"username":"other","password_hash":"` + otherHash + `","role":"viewer","active":1,"must_change_password":0,"pushplus_token":"","push_token_id":null}]}}`

	status, out := postBackup(t, server, cookie, csrf, "old.json", []byte(file), "replace")
	if status != http.StatusOK {
		t.Fatalf("restore status %d: %v", status, out)
	}

	// The old password still works, and the account is still an administrator.
	var hash, role string
	if err := database.QueryRow(`SELECT password_hash,role FROM users WHERE username='admin'`).Scan(&hash, &role); err != nil {
		t.Fatalf("the administrator disappeared: %v", err)
	}
	if hash == otherHash {
		t.Error("the administrator password was replaced by the one in the backup")
	}
	if role != "admin" {
		t.Errorf("role = %q, want admin", role)
	}
	// The session survived, so the page can keep working after a restore.
	if _, err := http.NewRequest(http.MethodGet, server.URL+"/api/me", nil); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("the session did not survive the restore: status %d", res.StatusCode)
	}
}

// TestBackupMergeKeepsExistingRows checks the gentler mode: it only adds what
// the file has and the database does not.
func TestBackupMergeKeepsExistingRows(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	if _, err := database.Exec(`INSERT INTO projects(name,customer) VALUES('现有项目','客户甲')`); err != nil {
		t.Fatal(err)
	}

	file := `{"meta":{"format":"` + backupFormat + `","version":1,"created_at":"2026-01-01T00:00:00+08:00","created_by":"boss","tables":[],"counts":{}},` +
		`"data":{"projects":[{"id":101,"name":"备份项目","customer":"客户乙","contact":"","notes":"","contract_no":"","owner":""}]}}`
	status, out := postBackup(t, server, cookie, csrf, "merge.json", []byte(file), "merge")
	if status != http.StatusOK {
		t.Fatalf("merge status %d: %v", status, out)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("projects = %d, want the existing one and the one from the file", count)
	}
	// The safety copy is only written for a replace.
	data := out["data"].(map[string]any)
	if safety, _ := data["safety_backup"].(string); safety != "" {
		t.Errorf("merge wrote a safety backup %q", safety)
	}
}

// TestBackupRestoreRejectsAFileThatIsNotABackup keeps a wrong file from
// wiping the database.
func TestBackupRestoreRejectsAFileThatIsNotABackup(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	if _, err := database.Exec(`INSERT INTO projects(name) VALUES('不要被删掉')`); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, body string }{
		{"broken.json", `{"meta":{"format":"` + backupFormat},
		{"other.json", `{"meta":{"format":"something-else","version":1},"data":{"projects":[]}}`},
		{"empty.json", `{"meta":{"format":"` + backupFormat + `","version":1},"data":{}}`},
		{"future.json", `{"meta":{"format":"` + backupFormat + `","version":99},"data":{"projects":[]}}`},
	}
	for _, tc := range cases {
		status, out := postBackup(t, server, cookie, csrf, tc.name, []byte(tc.body), "replace")
		if status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%v)", tc.name, status, out)
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM projects WHERE name='不要被删掉'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("a rejected file changed the database")
	}
}

// TestBackupRestoreIsAllOrNothing checks that a file which fails halfway
// leaves the database as it was.
func TestBackupRestoreIsAllOrNothing(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	if _, err := database.Exec(`INSERT INTO projects(name) VALUES('原来的项目')`); err != nil {
		t.Fatal(err)
	}
	// The device points at a project that is not in the file, so the foreign
	// key fails after the projects table has already been written.
	file := `{"meta":{"format":"` + backupFormat + `","version":1,"created_at":"2026-01-01T00:00:00+08:00","created_by":"boss","tables":[],"counts":{}},` +
		`"data":{"projects":[{"id":500,"name":"新项目","customer":"","contact":"","notes":"","contract_no":"","owner":""}],` +
		`"devices":[{"id":900,"project_id":999,"vendor":"Acme","model":"X","serial_number":"SN-ORPHAN","maintenance_start":"2026-01-01","maintenance_end":"2026-12-31","status":"active","notes":"","owner":"","contract_no":"","strategy_override":0}]}}`
	status, out := postBackup(t, server, cookie, csrf, "orphan.json", []byte(file), "replace")
	if status == http.StatusOK {
		t.Fatalf("the orphan device was accepted: %v", out)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM projects WHERE name='原来的项目'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("a failed restore left the database changed")
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM projects WHERE name='新项目'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("a failed restore wrote the projects of the file")
	}
}

// TestBackupInfoDescribesWhatWouldBeSaved checks the counts the panel shows
// before a download.
func TestBackupInfoDescribesWhatWouldBeSaved(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	if _, err := database.Exec(`INSERT INTO projects(name) VALUES('A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO projects(name) VALUES('B')`); err != nil {
		t.Fatal(err)
	}
	status, out := do(t, server, cookie, csrf, http.MethodGet, "/api/backup/info", nil)
	if status != http.StatusOK {
		t.Fatalf("info status %d", status)
	}
	data := out["data"].(map[string]any)
	counts := data["counts"].([]any)
	seen := map[string]float64{}
	for _, item := range counts {
		row := item.(map[string]any)
		seen[row["table"].(string)] = row["count"].(float64)
	}
	if seen["projects"] != 2 {
		t.Errorf("projects count = %v, want 2", seen["projects"])
	}
	if _, ok := data["safety"].([]any); !ok {
		t.Errorf("info has no safety list: %v", data["safety"])
	}
}

// TestBackupDownloadIsNotCached makes sure a browser never hands back an old
// file for a new download.
func TestBackupDownloadIsNotCached(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, _ := login(t, server, "admin", "SuperSecret12345")
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/backup", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	disposition := res.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment") || !strings.Contains(disposition, ".json") {
		t.Errorf("Content-Disposition = %q", disposition)
	}
}

// TestBackupRequiresAdmin keeps the whole database out of a viewer's reach.
func TestBackupRequiresAdmin(t *testing.T) {
	server, database := newTestServerWithDB(t)
	hash, err := auth.HashPassword("ViewerSecret12345")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO users(username,password_hash,role,active) VALUES('viewer',?, 'viewer',1)`, hash); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := login(t, server, "viewer", "ViewerSecret12345")
	status, _ := do(t, server, cookie, csrf, http.MethodGet, "/api/backup", nil)
	if status != http.StatusForbidden {
		t.Errorf("viewer backup status %d, want 403", status)
	}
	status, _ = postBackup(t, server, cookie, csrf, "x.json", []byte(`{}`), "replace")
	if status != http.StatusForbidden {
		t.Errorf("viewer restore status %d, want 403", status)
	}
}
