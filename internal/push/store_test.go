package push

import (
	"path/filepath"
	"testing"

	"maintenance-reminder/internal/db"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "push.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewStore(database)
}

func TestCreateAndReadToken(t *testing.T) {
	store := newStore(t)
	in := Input{Name: "运维组", Kind: KindUser, Token: "token-abc", Channel: "wechat", Template: "html", Enabled: true}
	if err := in.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	id, err := store.Create(in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Token != "token-abc" {
		t.Errorf("expected the token to round-trip, got %q", got.Token)
	}
	if got.Masked == "token-abc" {
		t.Error("expected the masked form to hide the secret")
	}
}

func TestGroupTokenRequiresTopicCode(t *testing.T) {
	in := Input{Name: "群组", Kind: KindGroup, Token: "token-abc"}
	if err := in.Normalize(); err == nil {
		t.Error("expected a group token without a topic code to be rejected")
	}
}

func TestUpdateKeepsExistingSecretWhenBlank(t *testing.T) {
	store := newStore(t)
	id, err := store.Create(Input{Name: "默认", Kind: KindUser, Token: "keep-me", Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	in := Input{Name: "默认", Kind: KindUser, Token: "", Enabled: true}
	if err = in.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err = store.Update(id, in); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Token != "keep-me" {
		t.Errorf("expected the secret to be preserved, got %q", got.Token)
	}
}

func TestDeleteRefusesReferencedToken(t *testing.T) {
	store := newStore(t)
	id, err := store.Create(Input{Name: "默认", Kind: KindUser, Token: "token-abc", Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err = store.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,push_token_id) VALUES('alice','x','admin',0,?)`, id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err = store.Delete(id); err == nil {
		t.Error("expected the delete to be refused while the token is referenced")
	}
	if _, err = store.DB.Exec(`DELETE FROM users WHERE username='alice'`); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if err = store.Delete(id); err != nil {
		t.Errorf("expected the delete to succeed once nothing references the token, got %v", err)
	}
}
