package internal

import (
	"context"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

var (
	pubsubPeerFilters []pubsub.PeerFilter
)

func (o *Otter) setupPubSub(ctx context.Context) error {
	psub, err := pubsub.NewGossipSub(ctx, o.p2p, pubsub.WithPeerFilter(o.syncerPubSubFilter))
	if err != nil {
		return fmt.Errorf("initing pubsub: %w", err)
	}
	o.pubsub = psub

	pubsubPeerFilters = append(pubsubPeerFilters, o.syncerPubSubFilter)

	return nil
}

func (o *Otter) pubsubChainFilter(peer peer.ID, topic string) bool {
	for _, filter := range pubsubPeerFilters {
		if !filter(peer, topic) {
			return false
		}
	}

	return true
}
