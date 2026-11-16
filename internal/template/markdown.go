package template

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// The markdown renderer. It covers the constructs a reminder actually uses:
// headings, emphasis, lists, quotes, code, rules, tables and paragraphs. It
// is kept small on purpose, so that the text shown in the preview is the text
// that the phone shows.
//
// The rules follow the usual markdown meaning:
//
//	# 一级标题      ## 二级标题
//	**粗体**        *斜体*        `代码`
//	- 无序列表      1. 有序列表
//	> 引用
//	---            分隔线
//	[文字](链接)
//	| 列一 | 列二 |   表格
//
// A blank line separates the blocks; a single newline inside a paragraph is
// kept as a line break.

var (
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	// A horizontal rule is three or more of the same character. The three
	// kinds are matched one by one, because the regexp engine has no
	// back-references.
	mdHRDash = regexp.MustCompile(`^\s*-{3,}\s*$`)
	mdHRStar = regexp.MustCompile(`^\s*\*{3,}\s*$`)
	mdHRLine = regexp.MustCompile(`^\s*_{3,}\s*$`)
	mdUL     = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	mdOL     = regexp.MustCompile(`^\s*(\d+)[.)]\s+(.*)$`)
	mdQuote  = regexp.MustCompile(`^\s*>\s?(.*)$`)
	mdBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	// mdBoldAlt needs a boundary at the end, so that a word of underscores
	// is not taken for a bold mark. The engine has no look ahead, so the
	// mark is taken back out afterwards.
	mdBoldAlt  = regexp.MustCompile(`__([^_\n]+)__([^_a-zA-Z0-9]|$)`)
	mdItalic   = regexp.MustCompile(`\*([^*\n]+)\*`)
	mdCode     = regexp.MustCompile("`([^`]+)`")
	mdLink     = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	mdSafeLink = regexp.MustCompile(`^(https?://|mailto:)`)
	// mdCodeMark finds the placeholders that stand for the code spans.
	mdCodeMark = regexp.MustCompile("\x00([0-9]+)\x00")
	// A table row is a line with pipes in it.
	mdTableRow = regexp.MustCompile(`^\s*\|(.+)\|\s*$`)
	// The line under the head of a table is made of dashes and colons.
	mdTableSep = regexp.MustCompile(`^\s*\|[\s:|-]+\|\s*$`)
)

// RenderMarkdown turns a markdown text into HTML. It is used by the preview,
// so that the user sees the message the way the phone will show it.
func RenderMarkdown(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	var out strings.Builder
	list := "" // the kind of list that is open, "" when there is none
	quote := false

	closeList := func() {
		if list != "" {
			out.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	closeQuote := func() {
		if quote {
			out.WriteString("</blockquote>\n")
			quote = false
		}
	}

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		// A table is a head row, a line of dashes and the body rows. The
		// head and the first body row are read together.
		if mdTableRow.MatchString(trimmed) && i+1 < len(lines) && mdTableSep.MatchString(strings.TrimSpace(lines[i+1])) {
			closeList()
			closeQuote()
			i = renderTable(&out, lines, i)
			continue
		}
		switch {
		case trimmed == "":
			closeList()
			closeQuote()
		case mdHRDash.MatchString(trimmed), mdHRStar.MatchString(trimmed), mdHRLine.MatchString(trimmed):
			closeList()
			closeQuote()
			out.WriteString("<hr>\n")
		case mdHeading.MatchString(trimmed):
			closeList()
			closeQuote()
			m := mdHeading.FindStringSubmatch(trimmed)
			level := len(m[1])
			out.WriteString("<h" + strconv.Itoa(level) + ">" + mdInline(m[2]) + "</h" + strconv.Itoa(level) + ">\n")
		case mdQuote.MatchString(trimmed):
			closeList()
			if !quote {
				out.WriteString("<blockquote>\n")
				quote = true
			}
			m := mdQuote.FindStringSubmatch(trimmed)
			out.WriteString("<p>" + mdInline(m[1]) + "</p>\n")
		case mdUL.MatchString(trimmed):
			closeQuote()
			if list != "ul" {
				closeList()
				out.WriteString("<ul>\n")
				list = "ul"
			}
			m := mdUL.FindStringSubmatch(trimmed)
			out.WriteString("<li>" + mdInline(m[1]) + "</li>\n")
		case mdOL.MatchString(trimmed):
			closeQuote()
			if list != "ol" {
				closeList()
				out.WriteString("<ol>\n")
				list = "ol"
			}
			m := mdOL.FindStringSubmatch(trimmed)
			out.WriteString("<li>" + mdInline(m[2]) + "</li>\n")
		default:
			closeList()
			out.WriteString("<p>" + mdInline(trimmed) + "</p>\n")
		}
	}
	closeList()
	closeQuote()
	return out.String()
}

// renderTable writes a table and returns the line of the last row it read,
// so that the caller can go on after it.
//
// A table that has no head row is written as a plain list of rows, because a
// table without a head cannot be shown as one.
func renderTable(out *strings.Builder, lines []string, i int) int {
	head := tableCells(lines[i])
	i++
	// The line of dashes is skipped.
	i++
	var body [][]string
	for i < len(lines) {
		row := strings.TrimSpace(lines[i])
		if !mdTableRow.MatchString(row) {
			break
		}
		body = append(body, tableCells(row))
		i++
	}
	if len(head) == 0 {
		return i - 1
	}
	out.WriteString("<table>\n<thead><tr>")
	for _, c := range head {
		out.WriteString("<th>" + mdInline(c) + "</th>")
	}
	out.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range body {
		out.WriteString("<tr>")
		for n := range head {
			cell := ""
			if n < len(row) {
				cell = row[n]
			}
			out.WriteString("<td>" + mdInline(cell) + "</td>")
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</tbody>\n</table>\n")
	return i - 1
}

// tableCells splits a row of a table into its cells.
func tableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	parts := strings.Split(row, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// mdInline renders the inline marks of one line of markdown.
//
// The code spans are taken out first, so that the marks inside them are
// left alone. The rest of the text is escaped, so that a text that was not
// meant to be html cannot break the preview.
func mdInline(s string) string {
	codes := []string{}
	// The code span is replaced by a mark that cannot occur in a text.
	s = mdCode.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, mdCode.FindStringSubmatch(m)[1])
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})

	s = html.EscapeString(s)
	s = mdBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = mdBoldAlt.ReplaceAllString(s, "<strong>$1</strong>$2")
	s = mdItalic.ReplaceAllString(s, "<em>$1</em>")
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		parts := mdLink.FindStringSubmatch(m)
		if !mdSafeLink.MatchString(parts[2]) {
			return parts[1]
		}
		return `<a href="` + parts[2] + `">` + parts[1] + `</a>`
	})

	// The code spans go back in, escaped as well.
	s = mdCodeMark.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(mdCodeMark.FindStringSubmatch(m)[1])
		if n < 0 || n >= len(codes) {
			return ""
		}
		return "<code>" + html.EscapeString(codes[n]) + "</code>"
	})
	return s
}

// RenderHTMLText turns the text of a message into the HTML the phone shows.
// A plain text keeps its line breaks, so that a message written as a list of
// lines is not folded into one paragraph.
func RenderHTMLText(text string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, html.EscapeString(line))
	}
	return strings.Join(out, "<br>\n")
}

// RenderAsHTML renders a text in the given format as HTML, for the preview.
func RenderAsHTML(format, text string) string {
	switch NormalizeFormat(format) {
	case FormatMarkdown:
		return RenderMarkdown(text)
	case FormatTxt:
		return RenderHTMLText(text)
	}
	// The HTML text is used as it is, but the marks that would break the
	// preview page are removed.
	return stripDangerousTags(text)
}

// The tags that would run code or load a remote page. Each kind is matched
// on its own, because the regexp engine has no back-references.
var dangerousKinds = []string{"script", "style", "iframe", "object", "embed", "link", "meta"}

// stripDangerousTags removes the tags that would run code or load a remote
// page, so that a preview of a message cannot do anything but show text.
func stripDangerousTags(text string) string {
	for _, kind := range dangerousKinds {
		// The pair of tags first, then a lonely opening tag.
		pair := regexp.MustCompile(`(?is)<\s*` + kind + `\b[^>]*>.*?<\s*/\s*` + kind + `\s*>`)
		text = pair.ReplaceAllString(text, "")
		lonely := regexp.MustCompile(`(?i)<\s*/?\s*` + kind + `\b[^>]*>`)
		text = lonely.ReplaceAllString(text, "")
	}
	return text
}
