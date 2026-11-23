package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"maintenance-reminder/internal/auth"
	"maintenance-reminder/internal/systemlog"
)

// A backup is one JSON document that carries the data the program holds, so
// that a copy can be taken before an upgrade and put back after a mistake.
//
// It is not a copy of the SQLite file: the rows are written one by one, which
// lets a file written by an older release be read by a newer one as long as
// the tables it knows about still exist. The trace of the program — the log,
// the sessions and the queue of pushes already sent — is left out, because
// restoring it would send the same reminders a second time.
const (
	backupFormat  = "maintenance-reminder-backup"
	backupVersion = 1
)

// backupTables lists the tables a backup carries, in the order they are
// written and restored. The order follows the foreign keys, so a row is
// always written after the row it points at and can be inserted back one at
// a time with the keys checked.
var backupTables = []string{
	"push_tokens",
	"users",
	"projects",
	"devices",
	"strategies",
	"device_strategies",
	"push_templates",
	"settings",
}

// backupLabels names the tables for the person reading the result, so the
// page never shows a table name.
var backupLabels = map[string]string{
	"push_tokens":       "推送令牌",
	"users":             "账号",
	"projects":          "项目",
	"devices":           "设备",
	"strategies":        "通知策略",
	"device_strategies": "设备与策略关联",
	"push_templates":    "推送模板",
	"settings":          "系统设置",
}

// backupMeta describes the file, so that a restore can tell where it came
// from and refuse a file that is not a backup of this program.
type backupMeta struct {
	Format    string         `json:"format"`
	Version   int            `json:"version"`
	CreatedAt string         `json:"created_at"`
	CreatedBy string         `json:"created_by"`
	Timezone  string         `json:"timezone"`
	Tables    []string       `json:"tables"`
	Counts    map[string]int `json:"counts"`
}

// backupBundle is the document after it has been read.
type backupBundle struct {
	Meta backupMeta
	Data map[string][]map[string]json.RawMessage
}

// backupCounts counts the rows of every table the backup carries.
func (s *Server) backupCounts(ctx context.Context) (map[string]int, error) {
	counts := make(map[string]int, len(backupTables))
	for _, table := range backupTables {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			return nil, err
		}
		counts[table] = n
	}
	return counts, nil
}

func (s *Server) backupMeta(ctx context.Context, actor string) (backupMeta, error) {
	counts, err := s.backupCounts(ctx)
	if err != nil {
		return backupMeta{}, err
	}
	return backupMeta{
		Format:    backupFormat,
		Version:   backupVersion,
		CreatedAt: time.Now().In(mustLocation(s.timezone())).Format(time.RFC3339),
		CreatedBy: actor,
		Timezone:  s.timezone(),
		Tables:    backupTables,
		Counts:    counts,
	}, nil
}

// writeBackup writes the whole document. The rows are streamed table by
// table, so a large database is never held in memory twice.
func (s *Server) writeBackup(ctx context.Context, w io.Writer, meta backupMeta) error {
	head, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, `{"meta":%s,"data":{`, head); err != nil {
		return err
	}
	for i, table := range meta.Tables {
		if i > 0 {
			if _, err = io.WriteString(w, ","); err != nil {
				return err
			}
		}
		if _, err = fmt.Fprintf(w, "%q:[", table); err != nil {
			return err
		}
		first := true
		err = queryRows(ctx, s.db, `SELECT * FROM `+table, func(row map[string]any) error {
			encoded, e := json.Marshal(row)
			if e != nil {
				return e
			}
			if !first {
				if _, e = io.WriteString(w, ","); e != nil {
					return e
				}
			}
			first = false
			_, e = w.Write(encoded)
			return e
		})
		if err != nil {
			return err
		}
		if _, err = io.WriteString(w, "]"); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, "}}")
	return err
}

// rowQuerier is what both the database and a transaction offer.
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryRows runs a query and hands every row to emit as a map of column name
// to value. The values keep their Go type, so a number stays a number in the
// file instead of becoming a string.
func queryRows(ctx context.Context, q rowQuerier, query string, emit func(map[string]any) error, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			return err
		}
		row := make(map[string]any, len(columns))
		for i, name := range columns {
			row[name] = plainValue(values[i])
		}
		if err = emit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

// plainValue turns a value the driver returned into something that encodes
// to the type it will be read back as.
func plainValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case int64, float64, bool, string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// exportBackup downloads the document.
func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	meta, err := s.backupMeta(r.Context(), currentUser(r).Username)
	if err != nil {
		errorResponse(w, err)
		return
	}
	stamp := time.Now().Format("20060102-150405")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// The plain name is what a browser that ignores filename* saves, the
	// escaped one keeps the Chinese name the page shows.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="maintenance-backup-%s.json"; filename*=UTF-8''%s`, stamp, url.PathEscape("维保备份-"+stamp+".json")))
	w.Header().Set("Cache-Control", "no-store")
	if err = s.writeBackup(r.Context(), w, meta); err != nil {
		// The status is already sent, so the failure can only be recorded.
		log.Printf("write backup: %v", err)
		return
	}
	s.auditMutation(r, systemlog.CategoryData, "backup_exported", "database", 0, backupDetail(meta.Counts))
}

// importBackup reads a file written by exportBackup and puts it back.
func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) {
	const maxBackupBytes = 128 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "BACKUP_TOO_LARGE", "备份文件超过 128 MB 限制")
			return
		}
		fail(w, http.StatusBadRequest, "BACKUP_FILE", "请选择有效的备份文件")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, "BACKUP_FILE", "请选择备份文件")
		return
	}
	defer file.Close()

	// "replace" puts the file back as it was, "merge" only adds what is
	// missing. Replacing is what a restore means, so it is the default.
	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode != "merge" {
		mode = "replace"
	}
	bundle, err := parseBackup(file)
	if err != nil {
		fail(w, http.StatusBadRequest, "BACKUP_INVALID", err.Error())
		return
	}

	// A restore that replaces the data keeps a copy of what is there now, so
	// that a file picked by mistake can be undone.
	safety := ""
	if mode == "replace" {
		if name, e := s.writeSafetyBackup(r.Context(), currentUser(r).Username); e != nil {
			log.Printf("write safety backup: %v", e)
		} else {
			safety = name
		}
	}

	restored, err := s.restoreBackup(r, bundle, mode)
	if err != nil {
		log.Printf("restore backup: %v", err)
		fail(w, http.StatusBadRequest, "BACKUP_RESTORE", "恢复失败，现有数据没有改变："+firstLine(err.Error()))
		return
	}
	s.auditMutation(r, systemlog.CategoryData, "backup_restored", "database", 0, fmt.Sprintf("方式=%s 备份时间=%s 恢复=%s", mode, bundle.Meta.CreatedAt, backupDetail(restored)))
	ok(w, map[string]any{
		"mode":          mode,
		"restored":      restored,
		"labels":        backupLabels,
		"safety_backup": safety,
		"source":        bundle.Meta,
	})
}

// parseBackup reads the document and checks that it is a backup of this
// program before anything is changed.
func parseBackup(r io.Reader) (*backupBundle, error) {
	var raw struct {
		Meta backupMeta                              `json:"meta"`
		Data map[string][]map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, errors.New("备份文件不是有效的 JSON，可能没有下载完整")
	}
	if raw.Meta.Format != backupFormat {
		return nil, errors.New("这不是本系统的备份文件")
	}
	if raw.Meta.Version <= 0 || raw.Meta.Version > backupVersion {
		return nil, fmt.Errorf("备份文件版本 %d 不受支持，请先升级程序", raw.Meta.Version)
	}
	if len(raw.Data) == 0 {
		return nil, errors.New("备份文件里没有数据")
	}
	return &backupBundle{Meta: raw.Meta, Data: raw.Data}, nil
}

// tableShape is the ordered column list of a table.
type tableShape struct {
	names []string
	set   map[string]bool
}

func shapeOf(ctx context.Context, tx *sql.Tx, table string) (tableShape, error) {
	shape := tableShape{set: map[string]bool{}}
	err := queryRows(ctx, tx, `PRAGMA table_info(`+table+`)`, func(row map[string]any) error {
		if name, ok := row["name"].(string); ok {
			shape.names = append(shape.names, name)
			shape.set[name] = true
		}
		return nil
	})
	return shape, err
}

// restoreBackup writes the rows back in one transaction, so that a file that
// turns out to be wrong leaves the database as it was.
func (s *Server) restoreBackup(r *http.Request, bundle *backupBundle, mode string) (map[string]int, error) {
	ctx := r.Context()
	user := currentUser(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// The account that runs the restore and its session are read first: the
	// rows of the users table are about to be replaced, and locking the
	// administrator out of the program would leave no way back in.
	account, err := readRow(ctx, tx, `SELECT * FROM users WHERE id=?`, user.ID)
	if err != nil {
		return nil, err
	}
	session, err := readRow(ctx, tx, `SELECT * FROM sessions WHERE token_hash=?`, sessionHash(r))
	if err != nil {
		return nil, err
	}

	if mode == "replace" {
		for i := len(backupTables) - 1; i >= 0; i-- {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+backupTables[i]); err != nil {
				return nil, err
			}
		}
	}

	restored := make(map[string]int, len(backupTables))
	for _, table := range backupTables {
		shape, err := shapeOf(ctx, tx, table)
		if err != nil {
			return nil, err
		}
		written := 0
		for _, row := range bundle.Data[table] {
			values := make(map[string]any, len(row))
			for name, value := range row {
				if shape.set[name] {
					values[name] = decodeCell(value)
				}
			}
			n, err := insertRow(ctx, tx, table, shape, values, mode == "merge")
			if err != nil {
				return nil, fmt.Errorf("%s：%w", table, err)
			}
			written += n
		}
		restored[table] = written
	}

	if err = keepCurrentAccount(ctx, tx, account, session); err != nil {
		return nil, err
	}
	return restored, tx.Commit()
}

// keepCurrentAccount makes sure the person who ran the restore can still
// sign in afterwards. The account they are using is kept exactly as it is
// now, so a backup taken with another password can never lock them out, and
// their session survives the replacement of the users table.
func keepCurrentAccount(ctx context.Context, tx *sql.Tx, account map[string]any, session map[string]any) error {
	username, _ := account["username"].(string)
	if username == "" {
		return nil
	}
	shape, err := shapeOf(ctx, tx, "users")
	if err != nil {
		return err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username=?`, username).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		values := make(map[string]any, len(account))
		for name, value := range account {
			if name != "id" {
				values[name] = value
			}
		}
		if _, err = insertRow(ctx, tx, "users", shape, values, false); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=?,role=?,active=?,must_change_password=?,pushplus_token=?,push_token_id=? WHERE id=?`,
			account["password_hash"], account["role"], account["active"], account["must_change_password"],
			account["pushplus_token"], account["push_token_id"], id); err != nil {
			return err
		}
	}
	if session == nil || id == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES(?,?,?,?)`,
		session["token_hash"], id, session["csrf_token"], session["expires_at"])
	return err
}

func sessionHash(r *http.Request) string {
	c, err := r.Cookie("maintenance_session")
	if err != nil {
		return ""
	}
	return auth.TokenHash(c.Value)
}

// readRow returns one row as a map, or nil when there is none.
func readRow(ctx context.Context, tx *sql.Tx, query string, args ...any) (map[string]any, error) {
	var row map[string]any
	err := queryRows(ctx, tx, query, func(x map[string]any) error {
		row = x
		return nil
	}, args...)
	return row, err
}

// insertRow writes one row and returns how many rows it wrote: a merge keeps
// the row that is already there, so the count can be zero.
func insertRow(ctx context.Context, tx *sql.Tx, table string, shape tableShape, values map[string]any, keepExisting bool) (int, error) {
	names := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	marks := make([]string, 0, len(values))
	for _, name := range shape.names {
		value, ok := values[name]
		if !ok {
			continue
		}
		names = append(names, name)
		args = append(args, value)
		marks = append(marks, "?")
	}
	if len(names) == 0 {
		return 0, nil
	}
	verb := "INSERT"
	if keepExisting {
		verb = "INSERT OR IGNORE"
	}
	statement := fmt.Sprintf(`%s INTO %s(%s) VALUES(%s)`, verb, table, strings.Join(names, ","), strings.Join(marks, ","))
	res, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// decodeCell turns the value read from the file back into the type the
// column holds.
func decodeCell(raw json.RawMessage) any {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil
	}
	if text[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return text
	}
	switch text {
	case "true":
		return int64(1)
	case "false":
		return int64(0)
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return f
	}
	return text
}

// The copy taken before a restore lives beside the database, so that it can
// be downloaded from the settings page and put back.
var safetyNamePattern = regexp.MustCompile(`^pre-restore-\d{8}-\d{6}\.json$`)

func (s *Server) backupDir() string {
	if strings.TrimSpace(s.dbPath) == "" {
		return filepath.Join(os.TempDir(), "maintenance-reminder-backups")
	}
	return filepath.Join(filepath.Dir(s.dbPath), "backups")
}

func (s *Server) writeSafetyBackup(ctx context.Context, actor string) (string, error) {
	meta, err := s.backupMeta(ctx, actor)
	if err != nil {
		return "", err
	}
	dir := s.backupDir()
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := "pre-restore-" + time.Now().Format("20060102-150405") + ".json"
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err = s.writeBackup(ctx, file, meta); err != nil {
		return "", err
	}
	pruneSafetyBackups(dir, 10)
	return name, nil
}

// pruneSafetyBackups keeps the newest few copies so that the folder does not
// grow without a limit.
func pruneSafetyBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && safetyNamePattern.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// backupInfo tells the page what a backup would carry right now, so that the
// counts can be shown before the file is downloaded.
func (s *Server) backupInfo(w http.ResponseWriter, r *http.Request) {
	counts, err := s.backupCounts(r.Context())
	if err != nil {
		errorResponse(w, err)
		return
	}
	items := make([]map[string]any, 0, len(backupTables))
	for _, table := range backupTables {
		items = append(items, map[string]any{"table": table, "label": backupLabels[table], "count": counts[table]})
	}
	safety := []map[string]any{}
	entries, err := os.ReadDir(s.backupDir())
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !safetyNamePattern.MatchString(entry.Name()) {
				continue
			}
			info, e := entry.Info()
			if e != nil {
				continue
			}
			safety = append(safety, map[string]any{"name": entry.Name(), "size": info.Size(), "created_at": info.ModTime().Format("2006-01-02 15:04:05")})
		}
	}
	sort.Slice(safety, func(i, j int) bool { return safety[i]["name"].(string) > safety[j]["name"].(string) })
	ok(w, map[string]any{"counts": items, "safety": safety, "format": backupFormat, "version": backupVersion})
}

func (s *Server) downloadSafetyBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !safetyNamePattern.MatchString(name) {
		fail(w, http.StatusBadRequest, "BACKUP_NAME", "备份文件名无效")
		return
	}
	path := filepath.Join(s.backupDir(), name)
	if _, err := os.Stat(path); err != nil {
		fail(w, http.StatusNotFound, "BACKUP_MISSING", "备份文件不存在")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

// backupDetail writes the counts of a backup as one line for the log.
func backupDetail(counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for _, table := range backupTables {
		if n, ok := counts[table]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", table, n))
		}
	}
	return strings.Join(parts, " ")
}

// firstLine keeps an error message to one line, so that it reads well in the
// notice the page shows.
func firstLine(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}
