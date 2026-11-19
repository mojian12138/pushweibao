package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"maintenance-reminder/internal/systemlog"
)

// listLogs returns the log entries that match the filter of the log page.
func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := systemlog.Query{
		Level:    strings.TrimSpace(q.Get("level")),
		Category: strings.TrimSpace(q.Get("category")),
		Search:   strings.TrimSpace(q.Get("search")),
		Actor:    strings.TrimSpace(q.Get("actor")),
		From:     strings.TrimSpace(q.Get("from")),
		To:       strings.TrimSpace(q.Get("to")),
		Order:    strings.TrimSpace(q.Get("order")),
		Limit:    atoiDefault(q.Get("limit"), 200),
		Offset:   atoiDefault(q.Get("offset"), 0),
	}
	// An unknown level or category would return an empty page without
	// saying why, so it is reported as a bad request instead.
	if query.Level != "" && !validLogLevel(query.Level) {
		fail(w, 400, "VALIDATION", "日志级别无效")
		return
	}
	if query.Category != "" && !validLogCategory(query.Category) {
		fail(w, 400, "VALIDATION", "日志分类无效")
		return
	}
	total, err := s.log().Count(query)
	if err != nil {
		errorResponse(w, err)
		return
	}
	entries, err := s.log().List(query)
	if err != nil {
		errorResponse(w, err)
		return
	}
	ok(w, map[string]any{"total": total, "items": entries, "limit": query.Limit, "offset": query.Offset})
}

// listLogMeta returns the values the filter offers, so that the log page can
// build its dropdowns without hardcoding them.
func (s *Server) listLogMeta(w http.ResponseWriter, r *http.Request) {
	actors := []string{}
	rows, err := s.db.Query(`SELECT DISTINCT actor FROM system_logs WHERE actor<>'' ORDER BY actor`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var a string
			if rows.Scan(&a) == nil {
				actors = append(actors, a)
			}
		}
	}
	ok(w, map[string]any{
		"levels":     []string{systemlog.LevelDebug, systemlog.LevelInfo, systemlog.LevelWarn, systemlog.LevelError},
		"categories": []string{systemlog.CategorySystem, systemlog.CategoryAuth, systemlog.CategoryPush, systemlog.CategorySchedule, systemlog.CategoryData, systemlog.CategoryStrategy},
		"actors":     actors,
	})
}

func (s *Server) log() *systemlog.Logger {
	return s.logger
}

// validLogLevel reports whether a level is one of the known ones.
func validLogLevel(level string) bool {
	switch level {
	case systemlog.LevelDebug, systemlog.LevelInfo, systemlog.LevelWarn, systemlog.LevelError:
		return true
	}
	return false
}

func validLogCategory(category string) bool {
	switch category {
	case systemlog.CategorySystem, systemlog.CategoryAuth, systemlog.CategoryPush, systemlog.CategorySchedule, systemlog.CategoryData, systemlog.CategoryStrategy:
		return true
	}
	return false
}

// atoiDefault reads a number from a query value, falling back to a default.
func atoiDefault(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return n
}
