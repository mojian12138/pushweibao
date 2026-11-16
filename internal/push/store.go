package push

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Token kinds accepted by PushPlus.
const (
	KindUser    = "user"
	KindMessage = "message"
	KindGroup   = "group"
)

// Token is a stored PushPlus credential with its delivery settings.
type Token struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Token       string `json:"-"`
	Masked      string `json:"token_masked"`
	TopicCode   string `json:"topic_code"`
	Channel     string `json:"channel"`
	Template    string `json:"template"`
	Option      string `json:"option"`
	CallbackURL string `json:"callback_url"`
	Enabled     bool   `json:"enabled"`
	Note        string `json:"note"`
	ExpiresAt   string `json:"expires_at"`
	CreatedAt   string `json:"created_at"`
}

// Store reads and writes push tokens.
type Store struct {
	DB *sql.DB
}

func NewStore(database *sql.DB) *Store { return &Store{DB: database} }

// List returns all tokens ordered by name.
func (s *Store) List() ([]Token, error) {
	rows, err := s.DB.Query(`SELECT id,name,kind,token_value,topic_code,channel,template,option,callback_url,enabled,note,expires_at,created_at FROM push_tokens ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		var t Token
		if err = rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Token, &t.TopicCode, &t.Channel, &t.Template, &t.Option, &t.CallbackURL, &t.Enabled, &t.Note, &t.ExpiresAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Masked = MaskToken(t.Token)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get returns one token by id.
func (s *Store) Get(id int64) (Token, error) {
	var t Token
	err := s.DB.QueryRow(`SELECT id,name,kind,token_value,topic_code,channel,template,option,callback_url,enabled,note,expires_at,created_at FROM push_tokens WHERE id=?`, id).Scan(&t.ID, &t.Name, &t.Kind, &t.Token, &t.TopicCode, &t.Channel, &t.Template, &t.Option, &t.CallbackURL, &t.Enabled, &t.Note, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return t, err
	}
	t.Masked = MaskToken(t.Token)
	return t, nil
}

// Input carries the writable fields of a token.
type Input struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Token       string `json:"token"`
	TopicCode   string `json:"topic_code"`
	Channel     string `json:"channel"`
	Template    string `json:"template"`
	Option      string `json:"option"`
	CallbackURL string `json:"callback_url"`
	Enabled     bool   `json:"enabled"`
	Note        string `json:"note"`
	ExpiresAt   string `json:"expires_at"`
}

// Normalize applies defaults and validates the enumerated fields.
func (in *Input) Normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Token = strings.TrimSpace(in.Token)
	in.TopicCode = strings.TrimSpace(in.TopicCode)
	in.Channel = strings.TrimSpace(in.Channel)
	in.Template = strings.ToLower(strings.TrimSpace(in.Template))
	in.Option = strings.TrimSpace(in.Option)
	in.CallbackURL = strings.TrimSpace(in.CallbackURL)
	if in.Name == "" {
		return errors.New("令牌名称必填")
	}
	if len(in.Name) > 64 {
		return errors.New("令牌名称不能超过 64 个字符")
	}
	switch in.Kind {
	case KindUser, KindMessage, KindGroup:
	case "":
		in.Kind = KindUser
	default:
		return errors.New("令牌类型只能是 user、message 或 group")
	}
	if !ValidChannel(in.Channel) {
		return fmt.Errorf("不支持的发送渠道：%s", in.Channel)
	}
	if !ValidTemplate(in.Template) {
		return fmt.Errorf("不支持的消息模板：%s", in.Template)
	}
	if in.Kind == KindGroup && in.TopicCode == "" {
		return errors.New("群组令牌必须填写群组编码")
	}
	return nil
}

// Create inserts a token and returns its id.
func (s *Store) Create(in Input) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO push_tokens(name,kind,token_value,topic_code,channel,template,option,callback_url,enabled,note,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, in.Name, in.Kind, in.Token, in.TopicCode, in.Channel, in.Template, in.Option, in.CallbackURL, boolInt(in.Enabled), in.Note, in.ExpiresAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Update rewrites a token. An empty token value keeps the stored secret.
func (s *Store) Update(id int64, in Input) error {
	value := in.Token
	if value == "" {
		var current string
		if err := s.DB.QueryRow(`SELECT token_value FROM push_tokens WHERE id=?`, id).Scan(&current); err != nil {
			return err
		}
		value = current
	}
	res, err := s.DB.Exec(`UPDATE push_tokens SET name=?,kind=?,token_value=?,topic_code=?,channel=?,template=?,option=?,callback_url=?,enabled=?,note=?,expires_at=? WHERE id=?`, in.Name, in.Kind, value, in.TopicCode, in.Channel, in.Template, in.Option, in.CallbackURL, boolInt(in.Enabled), in.Note, in.ExpiresAt, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetEnabled turns a token on or off.
func (s *Store) SetEnabled(id int64, enabled bool) error {
	res, err := s.DB.Exec(`UPDATE push_tokens SET enabled=? WHERE id=?`, boolInt(enabled), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// References reports how many rows point at the token, so the UI can warn
// before a deletion.
func (s *Store) References(id int64) (users int, strategies int, outbox int, err error) {
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE push_token_id=?`, id).Scan(&users)
	if err != nil {
		return
	}
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM strategies WHERE token_id=?`, id).Scan(&strategies); err != nil {
		return
	}
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE token_id=?`, id).Scan(&outbox)
	return
}

// Delete removes a token that is not referenced anywhere.
func (s *Store) Delete(id int64) error {
	users, strategies, outbox, err := s.References(id)
	if err != nil {
		return err
	}
	if users+strategies+outbox > 0 {
		return fmt.Errorf("该令牌仍被 %d 个账号、%d 个策略和 %d 条推送记录引用，无法删除", users, strategies, outbox)
	}
	res, err := s.DB.Exec(`DELETE FROM push_tokens WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ForUser returns the token bound to a username, if any.
func (s *Store) ForUser(username string) (Token, error) {
	var t Token
	err := s.DB.QueryRow(`SELECT t.id,t.name,t.kind,t.token_value,t.topic_code,t.channel,t.template,t.option,t.callback_url,t.enabled,t.note,t.expires_at,t.created_at FROM users u JOIN push_tokens t ON t.id=u.push_token_id WHERE u.username=?`, username).Scan(&t.ID, &t.Name, &t.Kind, &t.Token, &t.TopicCode, &t.Channel, &t.Template, &t.Option, &t.CallbackURL, &t.Enabled, &t.Note, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return t, err
	}
	t.Masked = MaskToken(t.Token)
	return t, nil
}

// Default returns the first enabled token marked as the fallback, if any.
func (s *Store) Default() (Token, error) {
	var t Token
	err := s.DB.QueryRow(`SELECT id,name,kind,token_value,topic_code,channel,template,option,callback_url,enabled,note,expires_at,created_at FROM push_tokens WHERE enabled=1 ORDER BY id LIMIT 1`).Scan(&t.ID, &t.Name, &t.Kind, &t.Token, &t.TopicCode, &t.Channel, &t.Template, &t.Option, &t.CallbackURL, &t.Enabled, &t.Note, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return t, err
	}
	t.Masked = MaskToken(t.Token)
	return t, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
