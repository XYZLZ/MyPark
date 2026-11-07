package auth

import (
	"context"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
)

// hashPassword derives the stored hash. Callers run it before opening a
// transaction so no pooled connection is held while waiting for the argon2id
// semaphore.
func hashPassword(ctx context.Context, h hashing.PasswordHasher, pw secret.String) (string, error) {
	hash, err := h.Hash(ctx, pw.Reveal())
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return hash, nil
}
