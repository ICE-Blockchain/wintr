// SPDX-License-Identifier: ice License 1.0

package firebaseauth

import (
	"context"

	firebaseAuth "firebase.google.com/go/v4/auth"
	"github.com/pkg/errors"

	"github.com/ice-blockchain/wintr/auth/internal"
)

// Public API.

var (
	ErrUserNotFound = errors.New("user not found")
	ErrConflict     = errors.New("change conflicts with another user")
)

type (
	Client interface {
		VerifyToken(ctx context.Context, token string) (*internal.Token, error)
		UpdateCustomClaims(ctx context.Context, userID string, customClaims map[string]any) error
		DeleteUser(ctx context.Context, userID string) error
		GetUser(ctx context.Context, userID string) (*firebaseAuth.UserRecord, error)
	}
)

// Private API.

type (
	auth struct {
		client *firebaseAuth.Client
	}

	config struct {
		WintrAuthFirebase struct {
			Credentials struct {
				FilePath    string `yaml:"filePath"`
				FileContent string `yaml:"fileContent"`
			} `yaml:"credentials" mapstructure:"credentials"`
		} `yaml:"wintr/auth/firebase" mapstructure:"wintr/auth/firebase"` //nolint:tagliatelle // Nope.
	}
)
