package otter

import (
	"context"

	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/keystore"

	"github.com/gorilla/mux"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.uber.org/zap"
)

type Otter interface {
	Protocols() Protocols
	Crypto() Cryptography
	// Settings()
	Logger(component string) *zap.Logger
	UI() UI

	HostID() peer.ID
	IPLD() ipld.DAGService

	ResolveOtterNodesForKey(context.Context, id.PublicID) ([]peer.ID, error)
}

type Storage interface {
	Get(ctx context.Context, k string) ([]byte, error)
	Put(ctx context.Context, k string, v []byte) error
	Search(ctx context.Context, prefix string) (<-chan []byte, error)
	Delete(ctx context.Context, k string) error
}

type StorageClasses interface {
	Public(pub id.PublicID) (Storage, error)
	Private(pk id.PrivateKey) (Storage, error)
	System() (Storage, error)
}

type Protocols interface {
	P2P() host.Host

	Registered() []protocol.ID
	RegisterP2PHandler(protocol protocol.ID, handler network.StreamHandler)
	UnregisterP2PHandler(protocol protocol.ID)

	RegisterPOISHandler(func(r *mux.Route))
	RegisterPOISHandlers(func(r *mux.Router))
}

type Cryptography interface {
	HostKey() (crypto.PrivKey, error)
	KeyStore() keystore.KeyStore
}
