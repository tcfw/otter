package keystore

import (
	"context"
	"crypto"

	"github.com/tcfw/otter/pkg/id"
)

type KeyStore interface {
	Keys(context.Context) ([]id.PublicID, error)

	Sign(context.Context, id.PublicID, []byte, crypto.Hash) ([]byte, error)

	PrivateSeal(context.Context, id.PublicID, []byte) ([]byte, error)

	PrivateUnseal(context.Context, id.PublicID, []byte) ([]byte, error)
}
