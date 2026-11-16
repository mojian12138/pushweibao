package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"maintenance-reminder/internal/config"
	"maintenance-reminder/internal/db"
	"maintenance-reminder/internal/push"
)

func newScheduler(t *testing.T) *Scheduler {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := config.Config{Timezone: "Asia/Shanghai"}
	return &Scheduler{DB: database, Config: cfg, Push: push.NewClient()}
}

func TestTargetsPrefersAccountToken(t *testing.T) {
	s := newScheduler(t)
	store := push.NewStore(s.DB)
	accountToken, err := store.Create(push.Input{Name: "账号令牌", Kind: push.KindUser, Token: "account-token", Enabled: true})
	if err != nil {
		t.Fatalf("create account token: %v", err)
	}
	strategyToken, err := store.Create(push.Input{Name: "策略令牌", Kind: push.KindUser, Token: "strategy-token", Enabled: true})
	if err != nil {
		t.Fatalf("create strategy token: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,active,push_token_id) VALUES('alice','x','admin',0,1,?)`, accountToken); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	targets, err := s.targets(context.Background(), strategyToken, []string{"alice"})
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Token.Token != "account-token" {
		t.Errorf("expected the account token to win, got %q", targets[0].Token.Token)
	}
}

func TestTargetsFallsBackToStrategyToken(t *testing.T) {
	s := newScheduler(t)
	store := push.NewStore(s.DB)
	strategyToken, err := store.Create(push.Input{Name: "策略令牌", Kind: push.KindUser, Token: "strategy-token", Enabled: true})
	if err != nil {
		t.Fatalf("create strategy token: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,active) VALUES('bob','x','admin',0,1)`); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	targets, err := s.targets(context.Background(), strategyToken, []string{"bob"})
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(targets) != 1 || targets[0].Token.Token != "strategy-token" {
		t.Fatalf("expected the strategy token to be used, got %+v", targets)
	}
}

func TestTargetsUseTopicForGroupTokens(t *testing.T) {
	s := newScheduler(t)
	store := push.NewStore(s.DB)
	groupToken, err := store.Create(push.Input{Name: "群组", Kind: push.KindGroup, Token: "group-token", TopicCode: "group-42", Enabled: true})
	if err != nil {
		t.Fatalf("create group token: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,active,push_token_id) VALUES('carol','x','admin',0,1,?)`, groupToken); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	targets, err := s.targets(context.Background(), 0, []string{"carol"})
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Topic != "group-42" {
		t.Errorf("expected the group topic, got %q", targets[0].Topic)
	}
	if targets[0].Recipient != "group-42" {
		t.Errorf("expected the group code as recipient, got %q", targets[0].Recipient)
	}
}

func TestTargetsDeduplicateGroupRecipients(t *testing.T) {
	s := newScheduler(t)
	store := push.NewStore(s.DB)
	groupToken, err := store.Create(push.Input{Name: "群组", Kind: push.KindGroup, Token: "group-token", TopicCode: "group-42", Enabled: true})
	if err != nil {
		t.Fatalf("create group token: %v", err)
	}
	for _, name := range []string{"dave", "erin"} {
		if _, err = s.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,active,push_token_id) VALUES(?, 'x','admin',0,1,?)`, name, groupToken); err != nil {
			t.Fatalf("insert user: %v", err)
		}
	}
	targets, err := s.targets(context.Background(), 0, []string{"dave", "erin"})
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected the shared group to collapse into 1 target, got %d", len(targets))
	}
}
