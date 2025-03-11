package metrics

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type PeerCollectorLastSet map[peer.ID]CollectorLastSet

type CollectorLastSet struct {
	Ts  time.Time
	Set Set
}
