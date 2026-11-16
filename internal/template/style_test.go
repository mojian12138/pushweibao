package template

import (
	"strings"
	"testing"
)

// TestRenderMarkdownKeepsTheMarks checks that the marks of a markdown text
// become the tags of the phone, so that the preview shows the same thing as
// the message does.
func TestRenderMarkdownKeepsTheMarks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"粗体", "**到期**：2026-12-31", []string{"<strong>到期</strong>"}},
		{"斜体", "*备注*", []string{"<em>备注</em>"}},
		{"标题", "## 维保提醒", []string{"<h2>维保提醒</h2>"}},
		{"列表", "- 第一项\n- 第二项", []string{"<ul>", "<li>第一项</li>"}},
		{"有序列表", "1. 第一项", []string{"<ol>", "<li>第一项</li>"}},
		{"引用", "> 请及时续保", []string{"<blockquote>", "请及时续保"}},
		{"分隔线", "---", []string{"<hr>"}},
		{"代码", "SN `SN-001`", []string{"<code>SN-001</code>"}},
		{"链接", "[文档](https://example.com/a)", []string{`href="https://example.com/a"`, ">文档</a>"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RenderMarkdown(c.in)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("the result %q is missing %q", got, w)
				}
			}
		})
	}
}

// TestRenderMarkdownTable checks that a table is read, because the monthly
// summary is a table.
func TestRenderMarkdownTable(t *testing.T) {
	in := "| 项目 | 剩余 |\n| :--- | :--- |\n| 市医院 | 30 天 |"
	got := RenderMarkdown(in)
	for _, w := range []string{"<table>", "<thead>", "<th>项目</th>", "<td>市医院</td>", "<td>30 天</td>", "</table>"} {
		if !strings.Contains(got, w) {
			t.Errorf("the result %q is missing %q", got, w)
		}
	}
}

// TestRenderMarkdownEscapesTheText checks that a text that was not meant to
// be html cannot break the preview.
func TestRenderMarkdownEscapesTheText(t *testing.T) {
	got := RenderMarkdown("危险 <script>alert(1)</script> 结束")
	if strings.Contains(got, "<script>") {
		t.Errorf("the script tag survived: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("the text was not escaped: %q", got)
	}
}

// TestRenderMarkdownKeepsTheCodeAsItIs checks that the marks inside a code
// span are left alone.
func TestRenderMarkdownKeepsTheCodeAsItIs(t *testing.T) {
	got := RenderMarkdown("值 `**不是粗体**`")
	if strings.Contains(got, "<strong>") {
		t.Errorf("the code span was rendered as bold: %q", got)
	}
	if !strings.Contains(got, "<code>**不是粗体**</code>") {
		t.Errorf("the code span was changed: %q", got)
	}
}

// TestPresetBodyHasNoMarkerLeft checks that the ready-made bodies of every
// format and kind can be rendered, so that a message that was not touched
// still goes out as a finished text.
func TestPresetBodyHasNoMarkerLeft(t *testing.T) {
	for _, f := range Formats {
		for _, kind := range []string{KindMilestone, KindMonthly, KindCycle} {
			body := DefaultBodyFor(f, kind)
			if strings.TrimSpace(body) == "" {
				t.Errorf("the body of %s/%s is empty", f, kind)
			}
			// The title must not carry a tag, because PushPlus shows a
			// title as plain text.
			title := DefaultTitleFor(f, kind)
			if strings.Contains(title, "<") {
				t.Errorf("the title of %s/%s carries a tag: %q", f, kind, title)
			}
			// Every marker of the body must be one the kind knows.
			for _, m := range markerPattern.FindAllStringSubmatch(body, -1) {
				key := m[1]
				known := KnownFor(kind, key, false)
				if kind == KindMonthly {
					known = known || KnownFor(KindMilestone, key, false)
				}
				if !known {
					t.Errorf("the body of %s/%s uses the unknown marker %q", f, kind, key)
				}
			}
		}
	}
}

// TestPresetHTMLLooksLikeACard checks that the html body is a card with a
// title bar and a table, so that the message looks tidy on the phone.
func TestPresetHTMLLooksLikeACard(t *testing.T) {
	body := DefaultBodyFor(FormatHTML, KindMilestone)
	for _, w := range []string{"border-radius", "<table", "background:#197d77", "维保到期提醒"} {
		if !strings.Contains(body, w) {
			t.Errorf("the html body is missing %q", w)
		}
	}
	// The item is a row of the table of the summary, so it starts with a
	// row and not with a list item.
	item := DefaultItemFor(FormatHTML)
	if !strings.HasPrefix(strings.TrimSpace(item), "<tr>") {
		t.Errorf("the html item is not a table row: %q", item)
	}
}

// TestPresetMarkdownLooksLikeATable checks that the markdown body is a
// table, so that the summary is easy to read.
func TestPresetMarkdownLooksLikeATable(t *testing.T) {
	body := DefaultBodyFor(FormatMarkdown, KindMilestone)
	if !strings.Contains(body, "| :--- |") {
		t.Errorf("the markdown body has no table: %q", body)
	}
	if !strings.Contains(body, "## ") {
		t.Errorf("the markdown body has no title: %q", body)
	}
	item := DefaultItemFor(FormatMarkdown)
	if !strings.HasPrefix(strings.TrimSpace(item), "|") {
		t.Errorf("the markdown item is not a table row: %q", item)
	}
}

// TestRenderAsHTMLShowsTheMarkdown checks that a markdown message is shown
// as html in the preview, so that the user sees the layout and not the
// source.
func TestRenderAsHTMLShowsTheMarkdown(t *testing.T) {
	got := RenderAsHTML(FormatMarkdown, "## 标题\n\n**到期**：2026-12-31")
	if !strings.Contains(got, "<h2>标题</h2>") {
		t.Errorf("the title was not rendered: %q", got)
	}
	if !strings.Contains(got, "<strong>到期</strong>") {
		t.Errorf("the bold text was not rendered: %q", got)
	}
}

// TestRenderAsHTMLRemovesTheDangerousTags checks that a preview of a
// message cannot do anything but show text.
func TestRenderAsHTMLRemovesTheDangerousTags(t *testing.T) {
	got := RenderAsHTML(FormatHTML, `<p>正文</p><script>alert(1)</script>`)
	if strings.Contains(got, "<script>") {
		t.Errorf("the script tag survived: %q", got)
	}
	if !strings.Contains(got, "<p>正文</p>") {
		t.Errorf("the text was removed as well: %q", got)
	}
}
