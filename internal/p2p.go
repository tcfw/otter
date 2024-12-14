package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/tcfw/otter/internal/version"
	v1api "github.com/tcfw/otter/pkg/api"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/ipns"

	"github.com/cenkalti/backoff/v4"
	libp2pIPNS "github.com/ipfs/boxo/ipns"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

var (
	defaultBootstrapPeers = []string{
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
		"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",         // mars.i.ipfs.io
		"/ip4/104.131.131.82/udp/4001/quic-v1/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ", // mars.i.ipfs.io
		// "/dns/home.tcfw.au/udp/23970/quic-v1/p2p/12D3KooWDfLpjhak6YQuG3ECNCXDHrzQaaTJSK43A3jX5QLbERdp",
	}
)

var connMgr, _ = connmgr.NewConnManager(50, 400, connmgr.WithGracePeriod(30*time.Second))

// Libp2pOptionsExtra provides some useful libp2p options
// to create a fully featured libp2p host. It can be used with
// SetupLibp2p.
var Libp2pOptionsExtra = []libp2p.Option{
	libp2p.ConnectionManager(connMgr),
	libp2p.EnableRelay(),
	libp2p.EnableRelayService(),
}

func (o *Otter) Registered() []protocol.ID {
	return o.p2p.Mux().Protocols()
}

func (o *Otter) RegisterP2PHandler(protocol protocol.ID, handler network.StreamHandler) {
	o.p2p.SetStreamHandler(protocol, handler)
}

func (o *Otter) UnregisterP2PHandler(protocol protocol.ID) {
	o.p2p.RemoveStreamHandler(protocol)
}

func (o *Otter) setupLibP2P(opts ...libp2p.Option) error {
	var ddht *dualdht.DHT
	var err error
	var transports = libp2p.DefaultTransports

	addrs := o.GetConfigAs([]string{}, config.P2P_ListenAddrs).([]string)
	listenAddrs := make([]multiaddr.Multiaddr, len(addrs))
	for i, addr := range addrs {
		a, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			return fmt.Errorf("parsing listen address: %w", err)
		}
		listenAddrs[i] = a
	}

	hostKey, err := o.HostKey()
	if err != nil {
		return fmt.Errorf("getting host key: %w", err)
	}

	scalingLimits := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&scalingLimits)
	scaledDefaultLimits := scalingLimits.AutoScale()

	cfg := rcmgr.PartialLimitConfig{
		System: rcmgr.ResourceLimits{
			// Allow unlimited outbound streams
			StreamsOutbound: rcmgr.Unlimited,
		},
	}

	limiter := rcmgr.NewFixedLimiter(cfg.Build(scaledDefaultLimits))

	o.rm, err = rcmgr.NewResourceManager(limiter)
	if err != nil {
		return fmt.Errorf("creating libp2p resource manager: %w", err)
	}

	finalOpts := []libp2p.Option{
		libp2p.ResourceManager(o.rm),
		libp2p.UserAgent("otter/" + version.Version()),
		libp2p.Identity(hostKey),
		libp2p.ListenAddrs(listenAddrs...),
		transports,
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			ddht, err = newDHT(o.ctx, h, o.ds)
			return ddht, err
		}),
		libp2p.EnableAutoRelayWithPeerSource(o.dhtPeerSource, autorelay.WithMinInterval(5*time.Minute)),
	}
	finalOpts = append(finalOpts, opts...)

	enableNat := o.GetConfigAs(true, config.P2P_NAT).(bool)
	if enableNat {
		o.logger.Info("enabling NAT services")

		finalOpts = append(finalOpts,
			libp2p.NATPortMap(),
			libp2p.EnableNATService(),
			libp2p.EnableHolePunching(),
		)
	}

	h, err := libp2p.New(
		finalOpts...,
	)
	if err != nil {
		return err
	}
	o.p2p = h
	o.dht = ddht

	return nil
}

func (o *Otter) stopP2P() error {
	o.dht.Close()
	o.p2p.Close()

	return nil
}

func newDHT(ctx context.Context, h host.Host, ds datastore.Batching) (*dualdht.DHT, error) {
	dhtOpts := []dualdht.Option{
		dualdht.DHTOption(dht.NamespacedValidator("pk", record.PublicKeyValidator{})),
		dualdht.DHTOption(dht.NamespacedValidator("ipns", libp2pIPNS.Validator{KeyBook: h.Peerstore()})),
		dualdht.DHTOption(dht.Concurrency(10)),
		dualdht.DHTOption(dht.Mode(dht.ModeAuto)),
	}
	if ds != nil {
		dhtOpts = append(dhtOpts, dualdht.DHTOption(dht.Datastore(ds)))
	}

	return dualdht.New(ctx, h, dhtOpts...)
}

func (o *Otter) Bootstrap(peers []peer.AddrInfo) {
	if len(peers) == 0 {
		ps, err := parseBootstrapPeers(defaultBootstrapPeers)
		if err != nil {
			panic(fmt.Errorf(`failed to parse hardcoded bootstrap peers: %w`, err))
		}
		peers = ps
	}

	connected := make(chan struct{})

	var wg sync.WaitGroup
	for _, pinfo := range peers {
		wg.Add(1)

		go func(pinfo peer.AddrInfo) {
			defer wg.Done()
			err := o.p2p.Connect(o.ctx, pinfo)
			if err != nil {
				fmt.Print(err)
				return
			}
			o.logger.Debug("Bootstrap to peer", zap.Any("peerID", pinfo.ID))
			connected <- struct{}{}
		}(pinfo)
	}

	go func() {
		wg.Wait()
		close(connected)
	}()

	i := 0
	for range connected {
		i++
	}
	if nPeers := len(peers); i < nPeers/2 {
		o.logger.Sugar().Warnf("only connected to %d bootstrap peers out of %d\n", i, nPeers)
	}

	err := o.dht.Bootstrap(o.ctx)
	if err != nil {
		fmt.Print(err)
		return
	}
}

func (o *Otter) ResolveOtterNodesForKey(ctx context.Context, pubk id.PublicID) ([]peer.ID, error) {
	p, err := pubk.AsLibP2P()
	if err != nil {
		return nil, fmt.Errorf("converting key: %w", err)
	}

	pid, err := peer.IDFromPublicKey(p)
	if err != nil {
		return nil, fmt.Errorf("getting id from pub key: %w", err)
	}

	ns := ipns.NameFromPeer(pid)
	rk := ns.RoutingKey()

	resCh, err := o.dht.SearchValue(ctx, string(rk))
	if err != nil {
		return nil, fmt.Errorf("getting DHT value: %w", err)
	}

	res := <-resCh

	rec, err := ipns.UnmarshalRecord(res)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling DHT value: %w", err)
	}

	if err := ipns.Validate(rec, p); err != nil {
		return nil, fmt.Errorf("validating IPNS record: %w", err)
	}

	return rec.OtterNodes()
}

func (o *Otter) apiHandle_Otter_Providers(w http.ResponseWriter, r *http.Request) {
	rawPublicID := r.URL.Query().Get("publicID")

	if rawPublicID == "" {
		apiJSONErrorWithStatus(w, errors.New("publicID required"), http.StatusBadRequest)
		return
	}

	nodes, err := o.ResolveOtterNodesForKey(r.Context(), id.PublicID(rawPublicID))
	if err != nil {
		apiJSONError(w, err)
		return
	}

	json.NewEncoder(w).Encode(nodes)
}

func (o *Otter) dhtPeerSource(ctx context.Context, num int) <-chan peer.AddrInfo {
	peerChan := make(chan peer.AddrInfo)

	go autoRelayFeeder(ctx, o.p2p, o.dht, peerChan)

	r := make(chan peer.AddrInfo)
	go func() {
		defer close(r)
		for ; num != 0; num-- {
			select {
			case v, ok := <-peerChan:
				if !ok {
					return
				}
				select {
				case r <- v:

				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return r
}

func autoRelayFeeder(ctx context.Context, h host.Host, dht *dualdht.DHT, peerChan chan<- peer.AddrInfo) {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 15 * time.Second
	bo.Multiplier = 3
	bo.MaxInterval = 1 * time.Hour
	bo.MaxElapsedTime = 0 // never stop
	t := backoff.NewTicker(bo)
	defer t.Stop()

	for {
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}

		tctx, cancel := context.WithTimeout(ctx, 10*time.Second)

		closestPeers, err := dht.WAN.GetClosestPeers(tctx, h.ID().String())
		if err != nil && err != context.DeadlineExceeded {
			// no-op: usually 'failed to find any peer in table' during startup
			cancel()
			continue
		}
		cancel()

		for _, p := range closestPeers {
			addrs := h.Peerstore().Addrs(p)
			if len(addrs) == 0 {
				continue
			}
			dhtPeer := peer.AddrInfo{ID: p, Addrs: addrs}
			select {
			case peerChan <- dhtPeer:
			case <-ctx.Done():
				return
			}
		}
	}
}

func parseBootstrapPeers(addrs []string) ([]peer.AddrInfo, error) {
	maddrs := make([]multiaddr.Multiaddr, len(addrs))
	for i, addr := range addrs {
		var err error
		maddrs[i], err = multiaddr.NewMultiaddr(addr)
		if err != nil {
			return nil, err
		}
	}
	return peer.AddrInfosFromP2pAddrs(maddrs...)
}

func (o *Otter) apiHandle_P2P_Peers(w http.ResponseWriter, r *http.Request) {
	ps := o.p2p.Network().Peerstore()

	resp := &v1api.PeerListResponse{}

	for _, peer := range o.p2p.Network().Peers() {
		pi := ps.PeerInfo(peer)
		if len(pi.Addrs) == 0 {
			continue
		}

		protos, err := ps.GetProtocols(peer)
		if err != nil {
			continue
		}

		resp.Peers = append(resp.Peers, v1api.PeerListResponse_PeerInfo{
			ID:        peer,
			Addrs:     pi.Addrs,
			Protocols: protos,
			Latency:   ps.LatencyEWMA(peer).String(),
		})
	}

	o.apiJSONResponse(w, resp)
}

func (o *Otter) setupMdns() error {
	n := &mDNSNotifee{o}

	o.mdns = mdns.NewMdnsService(o.p2p, "", n)

	if err := o.mdns.Start(); err != nil {
		return fmt.Errorf("starting mdns service: %w", err)
	}

	o.logger.Debug("started mDNS discovery")

	return nil
}

type mDNSNotifee struct {
	o *Otter
}

func (m *mDNSNotifee) HandlePeerFound(peer peer.AddrInfo) {
	m.o.logger.Debug("found mDNS peer", zap.Any("peer", peer.ID.String()))

	ctx, cancel := context.WithTimeout(m.o.ctx, 10*time.Second)
	defer cancel()

	m.o.p2p.Connect(ctx, peer)
}
