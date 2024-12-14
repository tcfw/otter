package api

import (
	"context"
	"errors"

	"github.com/tcfw/otter/pkg/id"
)

type authContextKey string

const (
	authID authContextKey = "auth.publicID"
)

var (
	ErrNoAuth               error = errors.New("no auth set")
	ErrUnknownAuthTokenType error = errors.New("unknown auth token type")
)

func GetAuthIDFromContext(ctx context.Context) (id.PublicID, error) {
	aid := ctx.Value(authID)
	if aid == nil {
		return "", ErrNoAuth
	}

	switch at := aid.(type) {
	case id.PublicID:
		return at, nil
	default:
		return "", ErrUnknownAuthTokenType
	}
}

func SetAuthIDForContext(ctx context.Context, pubk id.PublicID) context.Context {
	return context.WithValue(ctx, authID, pubk)
}
