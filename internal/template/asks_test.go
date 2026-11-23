package template

import "testing"

// The templates are shared, so the renderer has to know what a body asks for
// before it can choose a layout. These tests pin that reading down, because
// a body that asks for a summary and is rendered device by device is exactly
// how raw markers reach the phone.

func TestAsksForSummary(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"## {{month}} 汇总\n\n共 {{count}} 台", true},
		{"{{upcoming_section}}", true},
		{"{{expired_section}}", true},
		{"{{items}}", true},
		{"{{expired}}", true},
		{"{{expired_count}}", true},
		// A body of one device does not ask for a summary.
		{"剩余 {{days}} 天，设备 {{sn}}", false},
		{"{{project}} 到 {{end}} 到期", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := AsksForSummary(tc.body); got != tc.want {
			t.Errorf("AsksForSummary(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestAsksForList(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"## 提醒\n\n{{milestone_items}}", true},
		{"{{upcoming_section}}", true},
		{"{{month}}", true},
		{"设备 {{sn}} 剩余 {{days}} 天", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := AsksForList(tc.body); got != tc.want {
			t.Errorf("AsksForList(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

// TestRenderAnyLeavesNoKnownMarkerBehind is the guarantee the feature rests
// on: whatever known marker a body carries, RenderAny replaces it.
func TestRenderAnyLeavesNoKnownMarkerBehind(t *testing.T) {
	device := SampleDevice()
	summary := Summary{
		Month: "2026-11", Count: 2, Items: "明细", Expired: "过期明细", ExpiredCount: 1,
		UpcomingSection: "即将到期区块", ExpiredSection: "已过期区块",
		Device: device, Days: 30,
	}
	body := "{{project}}{{customer}}{{contact}}{{vendor}}{{model}}{{sn}}{{start}}{{end}}{{days}}{{notes}}{{owner}}{{device}}" +
		"{{milestone_items}}{{month}}{{count}}{{items}}{{expired}}{{expired_count}}{{upcoming_section}}{{expired_section}}"
	got := RenderAny(body, device, 30, "分组列表", summary)
	if containsMarker(got) {
		t.Errorf("a known marker survived RenderAny: %q", got)
	}
}

// TestRenderAlwaysLeavesSummaryMarkersBehind documents why a summary body may
// never reach the single-device renderer. The check keeps the caller honest:
// AsksForList is what keeps a body like this off that path.
func TestRenderAlwaysLeavesSummaryMarkersBehind(t *testing.T) {
	body := "## {{month}}\n\n{{upcoming_section}}"
	if !containsMarker(Render(body, SampleDevice(), 30)) {
		t.Skip("Render learned the summary markers; AsksForList may no longer be needed")
	}
	if !AsksForList(body) {
		t.Error("a body that keeps markers after Render is not reported as a list")
	}
}

func containsMarker(text string) bool {
	for _, m := range markerPattern.FindAllStringSubmatch(text, -1) {
		if KnownAnywhere(m[1]) {
			return true
		}
	}
	return false
}