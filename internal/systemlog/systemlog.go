// Package systemlog records the events that happen across the application.
//
// The log is meant to answer the question "what did the system do, and when"
// without digging through the push table alone. Every write goes to the
// database so that the log page can filter, search and sort.
package systemlog

import (
	"database/sql"
	"strings"
	"time"
)

// Levels, ordered from least to most severe.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// Categories group the events so that the log page can narrow the view.
const (
	CategorySystem   = "system"
	CategoryAuth     = "auth"
	CategoryPush     = "push"
	CategorySchedule = "schedule"
	CategoryData     = "data"
	CategoryStrategy = "strategy"
)

// Entry is one recorded event.
type Entry struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
	Level     string `json:"level"`
	Category  string `json:"category"`
	Action    string `json:"action"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	Actor     string `json:"actor"`
	RefType   string `json:"ref_type"`
	RefID     int64  `json:"ref_id"`
	IP        string `json:"ip"`
}

// Logger writes entries to the database. A zero Logger is not usable.
type Logger struct {
	DB *sql.DB
}

// Record appends an entry. Recording a log entry must never break the
// operation that produced it, so errors are returned for the caller to
// ignore or report, never panicked.
func (l *Logger) Record(e Entry) error {
	if l == nil || l.DB == nil {
		return nil
	}
	level := e.Level
	if level == "" {
		level = LevelInfo
	}
	category := e.Category
	if category == "" {
		category = CategorySystem
	}
	_, err := l.DB.Exec(`INSERT INTO system_logs(level,category,action,message,detail,actor,ref_type,ref_id,ip) VALUES(?,?,?,?,?,?,?,?,?)`,
		level, category, e.Action, e.Message, e.Detail, e.Actor, e.RefType, e.RefID, e.IP)
	return err
}

// Info records an informational event.
func (l *Logger) Info(category, action, message string) {
	_ = l.Record(Entry{Level: LevelInfo, Category: category, Action: action, Message: message})
}

// Warn records a recoverable problem.
func (l *Logger) Warn(category, action, message string) {
	_ = l.Record(Entry{Level: LevelWarn, Category: category, Action: action, Message: message})
}

// Error records a failure.
func (l *Logger) Error(category, action, message string) {
	_ = l.Record(Entry{Level: LevelError, Category: category, Action: action, Message: message})
}

// Query is the filter of the log page.
type Query struct {
	// Level filters to one level when set.
	Level string
	// Category filters to one category when set.
	Category string
	// Search matches the action, the message or the detail.
	Search string
	// Actor filters to one account when set.
	Actor string
	// From and To bound the time, using the "2006-01-02 15:04:05" layout.
	From string
	To   string
	// Order is "asc" or "desc" on the time.
	Order string
	// Limit caps the number of rows returned.
	Limit int
	// Offset skips rows for paging.
	Offset int
}

// Count returns the number of rows the filter matches.
func (l *Logger) Count(q Query) (int64, error) {
	where, args := q.build()
	var n int64
	err := l.DB.QueryRow(`SELECT COUNT(*) FROM system_logs `+where, args...).Scan(&n)
	return n, err
}

// List returns the entries the filter matches, newest first by default.
func (l *Logger) List(q Query) ([]Entry, error) {
	where, args := q.build()
	order := "DESC"
	if strings.EqualFold(q.Order, "asc") {
		order = "ASC"
	}
	limit := q.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := `SELECT id,created_at,level,category,action,message,detail,actor,ref_type,ref_id,ip FROM system_logs ` +
		where + ` ORDER BY id ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, limit, q.Offset)
	rows, err := l.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.CreatedAt, &e.Level, &e.Category, &e.Action, &e.Message, &e.Detail, &e.Actor, &e.RefType, &e.RefID, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// build turns the filter into a where clause and its arguments. The values
// are always passed as arguments, never pasted into the SQL.
func (q Query) build() (string, []any) {
	clauses := []string{"1=1"}
	args := []any{}
	if q.Level != "" {
		clauses = append(clauses, "level=?")
		args = append(args, q.Level)
	}
	if q.Category != "" {
		clauses = append(clauses, "category=?")
		args = append(args, q.Category)
	}
	if q.Actor != "" {
		clauses = append(clauses, "actor=?")
		args = append(args, q.Actor)
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		clauses = append(clauses, "(action LIKE ? OR message LIKE ? OR detail LIKE ?)")
		like := "%" + search + "%"
		args = append(args, like, like, like)
	}
	if q.From != "" {
		clauses = append(clauses, "created_at>=?")
		args = append(args, q.From)
	}
	if q.To != "" {
		clauses = append(clauses, "created_at<=?")
		args = append(args, q.To)
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

// Now returns the timestamp format used by the log page.
func Now() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
