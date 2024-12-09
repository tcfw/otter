package keystore

import (
	"context"

	"github.com/tcfw/otter/pkg/id"
)

type KeyStore interface {
	Keys(context.Context) ([]id.PublicID, error)
}
