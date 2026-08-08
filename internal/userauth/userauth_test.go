package userauth

import (
	"context"
	"testing"

	"github.com/adescoteaux1/generate-control-tower/internal/store"
	"github.com/adescoteaux1/generate-control-tower/internal/storetest"
)

func TestRegister_CreatesUserWithToken(t *testing.T) {
	st := storetest.New()

	user, err := Register(context.Background(), st, "ally@example.com", "001234567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Token == "" {
		t.Fatal("expected a non-empty token")
	}
	if user.ID == "" {
		t.Fatal("expected a non-empty user ID")
	}

	fetched, err := st.GetUserByToken(context.Background(), user.Token)
	if err != nil {
		t.Fatalf("expected to look up the new user by token: %v", err)
	}
	if fetched.Email != "ally@example.com" {
		t.Errorf("expected email to round-trip, got %q", fetched.Email)
	}
}

func TestRegister_RejectsDuplicateEmail(t *testing.T) {
	st := storetest.New()

	if _, err := Register(context.Background(), st, "ally@example.com", "001234567"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := Register(context.Background(), st, "ally@example.com", "999999999"); err != store.ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists for a duplicate email, got %v", err)
	}
}

func TestLogin_ReturnsFreshTokenOnMatchingCredentials(t *testing.T) {
	st := storetest.New()
	registered, err := Register(context.Background(), st, "ally@example.com", "001234567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loggedIn, err := Login(context.Background(), st, "ally@example.com", "001234567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loggedIn.ID != registered.ID {
		t.Errorf("expected login to return the same user, got different ID")
	}
	if loggedIn.Token == registered.Token {
		t.Errorf("expected login to rotate the token, got the same one back")
	}

	// The old token should no longer resolve; the new one should.
	if _, err := st.GetUserByToken(context.Background(), registered.Token); err != store.ErrNotFound {
		t.Errorf("expected old token to be invalidated, got err=%v", err)
	}
	if _, err := st.GetUserByToken(context.Background(), loggedIn.Token); err != nil {
		t.Errorf("expected new token to resolve, got err=%v", err)
	}
}

func TestLogin_RejectsWrongNUID(t *testing.T) {
	st := storetest.New()
	if _, err := Register(context.Background(), st, "ally@example.com", "001234567"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := Login(context.Background(), st, "ally@example.com", "wrong-nuid"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_RejectsUnknownEmail(t *testing.T) {
	st := storetest.New()

	if _, err := Login(context.Background(), st, "nobody@example.com", "001234567"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}
