package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"maintenance-reminder/internal/auth"
	"maintenance-reminder/internal/config"
	assets "maintenance-reminder/web"
)

type Server struct {
	db     *sql.DB
	dbPath string
	cfg    config.Config
	assets fs.FS
	mu     sync.Mutex
	fails  map[string][]time.Time
}

type envelope struct {
	OK    bool      `json:"ok"`
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type User struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password"`
	CSRF               string `json:"csrf_token,omitempty"`
}
type contextKey string

const userKey contextKey = "user"

func New(database *sql.DB, dbPath string, cfg config.Config) *Server {
	return &Server{db: database, dbPath: dbPath, cfg: cfg, assets: assets.FS(), fails: map[string][]time.Time{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, envelope{OK: true, Data: map[string]string{"status": "ok"}})
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.Handle("POST /api/logout", s.requireUser(http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/me", s.requireUser(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/password", s.requireUser(http.HandlerFunc(s.changePassword)))
	mux.Handle("GET /api/projects", s.requireUser(http.HandlerFunc(s.listProjects)))
	mux.Handle("POST /api/projects", s.requireAdmin(http.HandlerFunc(s.createProject)))
	mux.Handle("PUT /api/projects/{id}", s.requireAdmin(http.HandlerFunc(s.updateProject)))
	mux.Handle("DELETE /api/projects/{id}", s.requireAdmin(http.HandlerFunc(s.deleteProject)))
	mux.Handle("GET /api/devices", s.requireUser(http.HandlerFunc(s.listDevices)))
	mux.Handle("POST /api/devices", s.requireAdmin(http.HandlerFunc(s.createDevice)))
	mux.Handle("PUT /api/devices/{id}", s.requireAdmin(http.HandlerFunc(s.updateDevice)))
	mux.Handle("DELETE /api/devices/{id}", s.requireAdmin(http.HandlerFunc(s.deleteDevice)))
	mux.Handle("GET /api/dashboard", s.requireUser(http.HandlerFunc(s.dashboard)))
	mux.Handle("GET /api/strategies", s.requireUser(http.HandlerFunc(s.listStrategies)))
	mux.Handle("POST /api/strategies", s.requireAdmin(http.HandlerFunc(s.createStrategy)))
	mux.Handle("PUT /api/strategies/{id}", s.requireAdmin(http.HandlerFunc(s.updateStrategy)))
	mux.Handle("DELETE /api/strategies/{id}", s.requireAdmin(http.HandlerFunc(s.deleteStrategy)))
	mux.Handle("GET /api/notifications", s.requireUser(http.HandlerFunc(s.listNotifications)))
	mux.Handle("POST /api/notifications/{id}/retry", s.requireAdmin(http.HandlerFunc(s.retryNotification)))
	mux.Handle("GET /api/settings", s.requireAdmin(http.HandlerFunc(s.getSettings)))
	mux.Handle("PUT /api/settings", s.requireAdmin(http.HandlerFunc(s.putSettings)))
	mux.Handle("GET /api/users", s.requireAdmin(http.HandlerFunc(s.listUsers)))
	mux.Handle("POST /api/users", s.requireAdmin(http.HandlerFunc(s.createUser)))
	mux.Handle("PUT /api/users/{id}", s.requireAdmin(http.HandlerFunc(s.updateUser)))
	mux.Handle("POST /api/projects/import", s.requireAdmin(http.HandlerFunc(s.importProjects)))
	mux.Handle("GET /api/projects/export.xlsx", s.requireAdmin(http.HandlerFunc(s.exportProjects)))
	mux.Handle("POST /api/devices/import", s.requireAdmin(http.HandlerFunc(s.importDevices)))
	mux.Handle("GET /api/devices/template.xlsx", s.requireAdmin(http.HandlerFunc(s.deviceTemplate)))
	mux.Handle("GET /api/projects/{id}/devices/template.xlsx", s.requireAdmin(http.HandlerFunc(s.projectDeviceTemplate)))
	mux.Handle("POST /api/projects/{id}/devices/import", s.requireAdmin(http.HandlerFunc(s.importProjectDevices)))
	mux.Handle("POST /api/devices/{id}/test-push", s.requireAdmin(http.HandlerFunc(s.testDevicePush)))
	mux.Handle("POST /api/strategies/{id}/test", s.requireAdmin(http.HandlerFunc(s.testStrategyPush)))
	mux.Handle("GET /api/export.csv", s.requireAdmin(http.HandlerFunc(s.exportCSV)))
	mux.Handle("GET /api/backup", s.requireAdmin(http.HandlerFunc(s.backup)))
	mux.Handle("GET /", s.static())
	return securityHeaders(mux)
}

func (s *Server) static() http.Handler {
	files := http.FileServer(http.FS(s.assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			http.StripPrefix("/static/", files).ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		f, err := s.assets.Open("index.html")
		if err != nil {
			http.Error(w, "embedded UI unavailable", 500)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, "index.html", time.Time{}, f.(io.ReadSeeker))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, envelope{OK: false, Error: &apiError{Code: code, Message: message}})
}
func ok(w http.ResponseWriter, data any) { writeJSON(w, http.StatusOK, envelope{OK: true, Data: data}) }
func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, csrf, err := s.session(r)
		if err != nil {
			fail(w, 401, "UNAUTHENTICATED", "请先登录")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.URL.Path != "/api/password" && u.MustChangePassword {
				fail(w, 403, "PASSWORD_CHANGE_REQUIRED", "请先修改初始密码")
				return
			}
			if r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("X-CSRF-Token") != csrf {
				fail(w, 403, "CSRF_INVALID", "请求校验失败")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Role != "admin" {
			fail(w, 403, "FORBIDDEN", "权限不足")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
func currentUser(r *http.Request) User { u, _ := r.Context().Value(userKey).(User); return u }
func (s *Server) session(r *http.Request) (User, string, error) {
	c, err := r.Cookie("maintenance_session")
	if err != nil {
		return User{}, "", err
	}
	var u User
	var csrf, expires string
	var active int
	err = s.db.QueryRow(`SELECT u.id,u.username,u.role,u.must_change_password,u.active,s.csrf_token,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, auth.TokenHash(c.Value)).Scan(&u.ID, &u.Username, &u.Role, &u.MustChangePassword, &active, &csrf, &expires)
	if err != nil {
		return User{}, "", err
	}
	t, e := time.Parse(time.RFC3339, expires)
	if e != nil || time.Now().After(t) || active != 1 {
		return User{}, "", errors.New("expired session")
	}
	return u, csrf, nil
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if !s.loginAllowed(ip) {
		fail(w, 429, "LOGIN_THROTTLED", "登录尝试过多，请稍后再试")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	var u User
	var hash string
	var active int
	err := s.db.QueryRow(`SELECT id,username,role,must_change_password,password_hash,active FROM users WHERE username=?`, strings.TrimSpace(req.Username)).Scan(&u.ID, &u.Username, &u.Role, &u.MustChangePassword, &hash, &active)
	if err != nil || active != 1 || !auth.CheckPassword(hash, req.Password) {
		s.recordFailure(ip)
		fail(w, 401, "INVALID_CREDENTIALS", "账号或密码错误")
		return
	}
	s.clearFailures(ip)
	token, err := auth.RandomToken(32)
	if err != nil {
		fail(w, 500, "INTERNAL", "无法创建会话")
		return
	}
	csrf, err := auth.RandomToken(24)
	if err != nil {
		fail(w, 500, "INTERNAL", "无法创建会话")
		return
	}
	expires := auth.Expiry(time.Now(), s.cfg.HTTP.SessionHours).UTC()
	if _, err = s.db.Exec(`INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES(?,?,?,?)`, auth.TokenHash(token), u.ID, csrf, expires.Format(time.RFC3339)); err != nil {
		fail(w, 500, "INTERNAL", "无法创建会话")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "maintenance_session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.HTTP.SecureCookie, SameSite: http.SameSiteStrictMode, Expires: expires})
	u.CSRF = csrf
	writeJSON(w, 200, envelope{OK: true, Data: u})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("maintenance_session"); e == nil {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, auth.TokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "maintenance_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.HTTP.SecureCookie, SameSite: http.SameSiteStrictMode})
	ok(w, map[string]bool{"logged_out": true})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	_, csrf, _ := s.session(r)
	u.CSRF = csrf
	ok(w, u)
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	u := currentUser(r)
	var hash string
	if s.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, u.ID).Scan(&hash) != nil || !auth.CheckPassword(hash, req.Current) {
		fail(w, 400, "INVALID_PASSWORD", "当前密码错误")
		return
	}
	next, e := auth.HashPassword(req.New)
	if e != nil {
		fail(w, 400, "WEAK_PASSWORD", e.Error())
		return
	}
	if _, e = s.db.Exec(`UPDATE users SET password_hash=?,must_change_password=0 WHERE id=?`, next, u.ID); e != nil {
		fail(w, 500, "INTERNAL", "密码更新失败")
		return
	}
	ok(w, map[string]bool{"changed": true})
}
func (s *Server) loginAllowed(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := time.Now().Add(-15 * time.Minute)
	kept := s.fails[ip][:0]
	for _, t := range s.fails[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	s.fails[ip] = kept
	return len(kept) < 10
}
func (s *Server) recordFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails[ip] = append(s.fails[ip], time.Now())
}
func (s *Server) clearFailures(ip string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.fails, ip) }
func errorResponse(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	fail(w, 500, "INTERNAL", "操作失败")
}
