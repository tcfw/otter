package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/ipfs/go-cid"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"go.uber.org/zap"
)

const (
	pubsubNsPrefix = "/otter-pubsub/"
)

var (
	pubsubPeerFilters []pubsub.PeerFilter
)

func (o *Otter) setupPubSub(ctx context.Context) error {
	psub, err := pubsub.NewGossipSub(ctx, o.p2p,
		pubsub.WithPeerFilter(o.pubsubChainFilter),
		pubsub.WithDiscovery(&DHTPubSubDiscovery{o}, pubsub.WithDiscoveryOpts(discovery.TTL(30*time.Second))),
		pubsub.WithSeenMessagesTTL(3*time.Minute),
	)
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

var _ discovery.Discovery = (*DHTPubSubDiscovery)(nil)

type DHTPubSubDiscovery struct {
	o *Otter
}

func (pd *DHTPubSubDiscovery) getCID(ns string) (cid.Cid, error) {
	mh, err := multihash.Sum([]byte(pubsubNsPrefix+ns), multihash.SHA2_256, multihash.DefaultLengths[multihash.SHA2_256])
	if err != nil {
		return cid.Undef, err
	}

	return cid.NewCidV1(uint64(multicodec.Raw), mh), nil
}

// FindPeers discovers peers providing a service
func (pd *DHTPubSubDiscovery) FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan peer.AddrInfo, error) {
	options := &discovery.Options{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	if options.Limit == 0 {
		options.Limit = 20
	}

	cid, err := pd.getCID(ns)
	if err != nil {
		return nil, err
	}

	pd.o.logger.Named("pubsub-dht").Debug("finding peers for pubsub topic", zap.Any("cid", cid))

	return pd.o.dht.FindProvidersAsync(ctx, cid, options.Limit), nil
}

// Advertise advertises a service
func (pd *DHTPubSubDiscovery) Advertise(ctx context.Context, ns string, opts ...discovery.Option) (time.Duration, error) {
	options := &discovery.Options{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return 0, err
		}
	}

	cid, err := pd.getCID(ns)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pd.o.logger.Named("pubsub-dht").Debug("advertising pubsub topic", zap.Any("cid", cid))

	if err := pd.o.dht.Provide(ctx, cid, true); err != nil {
		return 0, err
	}

	return 5 * time.Minute, nil
}
