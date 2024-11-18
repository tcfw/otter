package otter

import (
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

type Otter interface {
	Protocols() Protocols
	Crypto() Cryptography
	Settings()
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
