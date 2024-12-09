package api

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

type PeerListResponse_PeerInfo struct {
	ID        peer.ID               `json:"id"`
	Addrs     []multiaddr.Multiaddr `json:"addrs"`
	Protocols []protocol.ID         `json:"protocols"`
	Latency   string                `json:"latency"`
}

type PeerListResponse struct {
	Peers []PeerListResponse_PeerInfo `json:"peers"`
}
