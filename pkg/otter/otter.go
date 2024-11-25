package otter

import (
	"context"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/tcfw/otter/pkg/id"
	"go.uber.org/zap"
)

type Otter interface {
	Protocols() Protocols
	Crypto() Cryptography
	// Settings()
	Logger(component string) *zap.Logger
	UI() UI
	HostID() peer.ID
}

type Storage interface {
	Get(ctx context.Context, k string) ([]byte, error)
	Put(ctx context.Context, k string, v []byte) error
	Search(ctx context.Context, prefix string) (<-chan []byte, error)
	Delete(ctx context.Context, k string) error
}

type StorageClasses interface {
	Public(pub id.PublicID) Storage
	Private(pk id.PrivateKey) Storage
}

type Protocols interface {
	Registered() []protocol.ID
	RegisterP2PHandler(protocol protocol.ID, handler network.StreamHandler)
	UnregisterP2PHandler(protocol protocol.ID)
}

type Cryptography interface {
	HostKey() (crypto.PrivKey, error)
	// KeyStore() keystore.KeyStore
}
