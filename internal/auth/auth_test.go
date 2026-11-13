package auth

import "testing"

func TestPasswordAndTokens(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("expected short password rejection")
	}
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse battery staple") || CheckPassword(hash, "wrong password") {
		t.Fatal("password verification failed")
	}
	a, err := RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || TokenHash(a) == a {
		t.Fatal("token generation/hash failed")
	}
}
