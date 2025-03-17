package otter

import (
	"context"

	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/keystore"

	"github.com/gorilla/mux"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.uber.org/zap"
)

type Otter interface {
	//Host ID of the underlying node
	HostID() peer.ID

	//Logger helper for specific component
	Logger(component string) *zap.Logger

	//Crypto helper functions
	Crypto() Cryptography

	//IPNS-based resolver for Otter Nodes via public keys
	ResolveOtterNodesForKey(ctx context.Context, id id.PublicID) ([]peer.ID, error)

	//Public DHT provider helper
	DHTProvide(ctx context.Context, key cid.Cid, announce bool) error

	//Lookup providers for a CID
	DHTSearchProviders(ctx context.Context, key cid.Cid, max int) ([]peer.AddrInfo, error)

	//Wait until the node has been fully bootstrapped
	WaitForBootstrap(ctx context.Context) chan struct{}

	//OS UI bridge
	UI() UI

	//P2P, API and POIS protocol handlers
	Protocols() Protocols

	//Private and public storage classes per key,
	// replicating to all of the key's nodes via CRDT
	Storage() StorageClasses

	//Public block storage per key,
	// distributing amoungst the key's nodes
	DistributedStorage(id.PublicID) (DistributedStorage, error)

	//Public node IPLD service from ipfs-lite
	//to the underlying node
	IPLD() ipld.DAGService

	// Settings()
}

type Storage interface {
	Get(ctx context.Context, k string) ([]byte, error)
	Put(ctx context.Context, k string, v []byte) error
	// Search(ctx context.Context, prefix string) (<-chan []byte, error)
	Delete(ctx context.Context, k string) error
}

type StorageClasses interface {
	Public(pub id.PublicID) (datastore.Batching, error)
	Private(pk id.PrivateKey) (datastore.Batching, error)
	PrivateFromPublic(pk id.PublicID) (datastore.Batching, error)
	System() (datastore.Batching, error)
}

type Protocols interface {
	//libp2p host
	P2P() host.Host

	//List of registered protocols
	Registered() []protocol.ID

	//Register a new libp2p protocol hander
	RegisterP2PHandler(protocol protocol.ID, handler network.StreamHandler)

	//Remove a libp2p protocol hander
	UnregisterP2PHandler(protocol protocol.ID)

	//Register a new plain-old-internet-service handler (e.g. HTTPs/SMTP/IMAP etc)
	//via a single route
	RegisterPOISHandler(func(r *mux.Route))

	//Register a new plain-old-internet-service handler (e.g. HTTPs/SMTP/IMAP etc)
	//via a router
	RegisterPOISHandlers(func(r *mux.Router))

	//Register local API handlers via a route
	RegisterAPIHandler(func(r *mux.Route))

	//Register local API handlers via a router
	RegisterAPIHandlers(func(r *mux.Router))
}

type Cryptography interface {
	//Returns the hode nodes private key
	HostKey() (crypto.PrivKey, error)

	//Identity key store
	KeyStore() keystore.KeyStore
}
