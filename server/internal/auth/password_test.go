package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash does not use argon2id: %s", hash)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Fatal("password did not verify")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("wrong password verified")
	}
}

func TestTokenHashDoesNotStoreRawToken(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	hash := HashToken(token)
	if hash == token {
		t.Fatal("token hash equals raw token")
	}
	if !VerifyToken(token, hash) {
		t.Fatal("token did not verify")
	}
	if VerifyToken(token+"x", hash) {
		t.Fatal("wrong token verified")
	}
}
