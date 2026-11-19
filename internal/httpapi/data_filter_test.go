package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestDeviceBucketFiltersMatchDashboardCounts(t *testing.T) {
	server, database := newTestServerWithDB(t)
	today := time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")
	projectID, err := database.Exec(`INSERT INTO projects(name) VALUES('Bucket project')`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := projectID.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	dates := []string{dateOffset(today, -1), dateOffset(today, 0), dateOffset(today, 10), dateOffset(today, 30), dateOffset(today, 45), dateOffset(today, 90), dateOffset(today, 100)}
	for i, end := range dates {
		if _, err = database.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status) VALUES(?,?,?,?,?,?,?)`, id, "Acme", "Model", fmt.Sprintf("SN%d", i), today, end, "active"); err != nil {
			t.Fatal(err)
		}
	}
	adminCookie, csrf := login(t, server, "admin", "SuperSecret12345")
	cases := []struct {
		bucket string
		want   int
	}{{"expired", 1}, {"soon30", 3}, {"soon90", 5}, {"active", 6}}
	for _, tc := range cases {
		status, out := do(t, server, adminCookie, csrf, http.MethodGet, "/api/devices?bucket="+tc.bucket, nil)
		if status != http.StatusOK {
			t.Fatalf("bucket %s: status %d", tc.bucket, status)
		}
		rows := out["data"].([]any)
		if len(rows) != tc.want {
			t.Errorf("bucket %s returned %d rows, want %d", tc.bucket, len(rows), tc.want)
		}
	}
	status, out := do(t, server, adminCookie, csrf, http.MethodGet, "/api/devices?q=ACM", nil)
	if status != http.StatusOK || len(out["data"].([]any)) != 7 {
		t.Fatalf("partial vendor search returned status=%d data=%v", status, out["data"])
	}
	_, dashboard := do(t, server, adminCookie, csrf, http.MethodGet, "/api/dashboard", nil)
	d := dashboard["data"].(map[string]any)
	for key, want := range map[string]float64{"expired": 1, "within_30_days": 3, "within_90_days": 5, "maintenance_active": 6} {
		if got := d[key].(float64); got != want {
			t.Errorf("dashboard %s=%v, want %v", key, got, want)
		}
	}
}

func dateOffset(today string, days int) string {
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		panic(err)
	}
	return t.AddDate(0, 0, days).Format("2006-01-02")
}
