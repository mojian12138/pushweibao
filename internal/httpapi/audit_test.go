package httpapi

import (
	"net/http"
	"testing"
)

func TestProjectMutationAppearsInSystemLog(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	status, created := do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "审计项目", "customer": "审计客户", "contact": "联系人", "owner": "负责人",
	})
	if status != http.StatusCreated {
		t.Fatalf("create project status=%d", status)
	}
	projectID := int64(created["data"].(map[string]any)["id"].(float64))
	status, logs := do(t, server, cookie, csrf, http.MethodGet, "/api/logs?category=data&search=project_created", nil)
	if status != http.StatusOK {
		t.Fatalf("query logs status=%d", status)
	}
	data := logs["data"].(map[string]any)
	items := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("log entries=%d, want 1", len(items))
	}
	entry := items[0].(map[string]any)
	if entry["actor"] != "admin" || entry["ref_type"] != "project" || int64(entry["ref_id"].(float64)) != projectID {
		t.Fatalf("unexpected audit record: %v", entry)
	}
	if got := entry["detail"].(string); got == "" {
		t.Fatal("audit detail is empty")
	}
}
