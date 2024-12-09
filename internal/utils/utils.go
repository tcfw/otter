package utils

import (
	"context"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
)

func FirstOnlinePeer(ctx context.Context, peers []peer.ID, h host.Host) (peer.ID, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	res := make(chan peer.ID)
	defer close(res)

	for _, peer := range peers {
		if peer == h.ID() {
			return peer, nil
		}

		go func() {
			<-ping.Ping(ctx, h, peer)

			select {
			case res <- peer:
			default:
			}
		}()
	}

	select {
	case <-ctx.Done():
		return peer.ID(""), ctx.Err()
	case peer := <-res:
		return peer, nil
	}
}
