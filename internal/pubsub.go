package internal

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	dicoRouter "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"go.uber.org/zap"
)

const (
	pubsubNsPrefix = "/otter-pubsub/"

	otterTopicPrefix = "/otter/"
)

var (
	pubsubPeerFilters []pubsub.PeerFilter
)

func (o *Otter) setupPubSub(ctx context.Context) error {
	disco := dicoRouter.NewRoutingDiscovery(o.dht)

	minBackoff, maxBackoff := time.Second*60, time.Hour
	rng := rand.New(rand.NewSource(rand.Int63()))
	d, err := backoff.NewBackoffDiscovery(
		disco,
		backoff.NewExponentialBackoff(minBackoff, maxBackoff, backoff.FullJitter, time.Second, 5.0, 0, rng),
	)
	if err != nil {
		return err
	}

	psub, err := pubsub.NewGossipSub(ctx, o.p2p,
		pubsub.WithPeerFilter(o.pubsubChainFilter),
		pubsub.WithDiscovery(d),
		pubsub.WithSeenMessagesTTL(20*time.Minute),
	)
	if err != nil {
		return fmt.Errorf("initing private pubsub: %w", err)
	}
	o.pubsub = psub

	pubsubPeerFilters = append(pubsubPeerFilters,
		o.syncerPubSubFilter,
		o.metricsPubSubFilter,

		//allow anything else
		func(pid peer.ID, topic string) bool { return !strings.HasPrefix(topic, otterTopicPrefix) },
	)

	return nil
}

func (o *Otter) pubsubChainFilter(peer peer.ID, topic string) bool {
	if !strings.HasPrefix(topic, otterTopicPrefix) {
		return true
	}

	o.logger.Debug("run filtering for pubsub peer", zap.String("topic", topic), zap.Any("peer", peer))

	for _, filter := range pubsubPeerFilters {
		if filter(peer, topic) {
			o.logger.Debug("peer allowed", zap.Any("topic", topic), zap.Any("peer", peer.String()))
			return true
		}
	}

	return false
}
