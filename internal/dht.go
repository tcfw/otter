package internal

import (
	"context"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (o *Otter) DHTProvide(ctx context.Context, key cid.Cid, announce bool) error {
	return o.dht.Provide(ctx, key, announce)
}

func (o *Otter) DHTSearchProviders(ctx context.Context, key cid.Cid, max int) (providers []peer.AddrInfo, err error) {
	ch := o.dht.FindProvidersAsync(ctx, key, max)

	for {
		select {
		case <-ctx.Done():
			return
		case p := <-ch:
			providers = append(providers, p)

			if len(providers) >= max {
				return
			}
		}
	}
}
