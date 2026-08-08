// Package userauth handles applicant registration and login: issuing and
// rotating the bearer tokens required by every other Control Tower endpoint.
package userauth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/adescoteaux1/generate-control-tower/internal/auth"
	"github.com/adescoteaux1/generate-control-tower/internal/models"
	"github.com/adescoteaux1/generate-control-tower/internal/store"
)

// ErrInvalidCredentials is returned when the email/NUID pair on login
// doesn't match a registered user.
var ErrInvalidCredentials = errors.New("email/NUID combination not found")

// Register creates a new user and issues their first bearer token. Returns
// store.ErrAlreadyExists if the email is already registered.
func Register(ctx context.Context, st store.Store, email, nuid string) (*models.User, error) {
	token, err := auth.GenerateToken()
	if err != nil {
		return nil, err
	}
	user := &models.User{
		ID:        uuid.NewString(),
		Email:     email,
		NUID:      nuid,
		Token:     token,
		CreatedAt: time.Now(),
	}
	if err := st.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// Login verifies email+NUID against an existing user and rotates their
// token, so a lost/leaked token can be replaced without re-registering.
func Login(ctx context.Context, st store.Store, email, nuid string) (*models.User, error) {
	user, err := st.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if user.NUID != nuid {
		return nil, ErrInvalidCredentials
	}

	token, err := auth.GenerateToken()
	if err != nil {
		return nil, err
	}
	if err := st.SetUserToken(ctx, user.ID, token); err != nil {
		return nil, err
	}
	user.Token = token
	return user, nil
}
