package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	ipfslite "github.com/hsanjuan/ipfs-lite"
	"github.com/ipfs/boxo/blockstore"
	pinner "github.com/ipfs/boxo/pinning/pinner"
	"github.com/ipfs/boxo/pinning/pinner/dspinner"
	"github.com/ipfs/go-datastore"
	ipld "github.com/ipfs/go-ipld-format"
	p2pforge "github.com/ipshipyard/p2p-forge/client"
	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/tcfw/otter/internal/ident4"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"go.uber.org/zap"
)

type Otter struct {
	ctx    context.Context
	logger *zap.Logger

	p2p host.Host
	dht *dualdht.DHT

	pubsub *pubsub.PubSub
	blocks blockstore.GCBlockstore
	ipld   ipld.DAGService
	pinner pinner.Pinner
	i4     *ident4.Ident4
	mdns   mdns.Service
	rm     network.ResourceManager
	ping   *ping.PingService

	ui *uiConnector
	ds datastore.Batching

	sc *StorageClasses

	autotlsCM *p2pforge.P2PForgeCertMgr

	apiRouter  *mux.Router
	poisRouter *mux.Router
}

func (o *Otter) Crypto() otter.Cryptography     { return o }
func (o *Otter) Protocols() otter.Protocols     { return o }
func (o *Otter) Logger(comp string) *zap.Logger { return o.logger.Named(comp) }
func (o *Otter) UI() otter.UI                   { return o.ui }
func (o *Otter) Storage() otter.StorageClasses  { return o.sc }
func (o *Otter) IPLD() ipld.DAGService          { return o.ipld }

func NewOtter(ctx context.Context, logger *zap.Logger) (*Otter, error) {
	o := &Otter{
		ctx:    ctx,
		logger: logger,
	}
	o.sc = &StorageClasses{o}

	ds, err := NewDiskDatastoreStorage(o)
	if err != nil {
		return nil, fmt.Errorf("starting datastore: %w", err)
	}
	o.ds = ds

	if err := o.setupLibP2P(); err != nil {
		return nil, fmt.Errorf("initing libp2p: %w", err)
	}

	ping.NewPingService(o.p2p)

	if err := o.setupPubSub(ctx); err != nil {
		return nil, fmt.Errorf("initing pubsub: %w", err)
	}

	if err := o.setupAPI(ctx); err != nil {
		return nil, fmt.Errorf("initing api: %w", err)
	}

	if err := o.setupPOISGW(ctx); err != nil {
		return nil, fmt.Errorf("initing pois gw: %w", err)
	}

	if err := o.setupMdns(); err != nil {
		o.logger.Error("setting up mDNS discovery service: %w", zap.Error(err))
	}

	o.blocks = blockstore.NewGCBlockstore(blockstore.NewBlockstore(ds), blockstore.NewGCLocker())

	ipfs, err := ipfslite.New(ctx, ds, o.blocks, o.p2p, o.dht, nil)
	if err != nil {
		return nil, fmt.Errorf("initing ipfs-lite: %w", err)
	}
	o.ipld = ipfs

	pinDS, err := otter.NewNamespacedStorage(ds, datastore.NewKey("pins"), o.logger.Named("pins"))
	if err != nil {
		return nil, err
	}

	o.pinner, err = dspinner.New(ctx, pinDS, o.ipld)
	if err != nil {
		return nil, err
	}

	i4, err := ident4.Setup(o.p2p, o.logger.Named("ident4"), o.Crypto())
	if err != nil {
		return nil, err
	}
	o.i4 = i4

	go o.watchPeers()
	go o.publishIPNS(ctx)

	go func() {
		sub, err := o.p2p.EventBus().Subscribe(&event.EvtLocalProtocolsUpdated{})
		if err != nil {
			o.logger.Error("getting eventbus sub for protocols", zap.Error(err))
			return
		}

		for evt := range sub.Out() {
			o.logger.Debug("Protocols updated", zap.Any("protocols", evt))
		}
	}()

	if err := startBuiltinPlugins(o); err != nil {
		o.logger.Error("loading builtin plugins", zap.Error(err))
	}

	loadExternalPlugins(o)

	go o.publishMetrics()
	go o.ensureSubsystems()

	return o, nil
}

func (o *Otter) Dial(peer peer.ID, proto protocol.ID, remote id.PublicID, local id.PublicID) (network.Stream, error) {
	return o.i4.Dial(peer, proto, remote, local)
}

func (o *Otter) DialContext(ctx context.Context, peer peer.ID, proto protocol.ID, remote id.PublicID, local id.PublicID) (network.Stream, error) {
	return o.i4.DialContext(ctx, peer, proto, remote, local)
}

func (o *Otter) watchPeers() {
	t := time.NewTicker(1 * time.Minute)

	for {
		select {
		case <-t.C:
		case <-o.ctx.Done():
			t.Stop()
			return
		}

		count := len(o.P2P().Network().Peers())

		o.logger.Debug("peer count", zap.Any("n", count))

		if count == 0 {
			o.logger.Debug("no peers. restarting bootstrap")
			o.Bootstrap(nil)
		}
	}
}

func loadExternalPlugins(o *Otter) {
	pluginLoadDirs := o.GetConfigAs([]string{}, config.Plugins_LoadDir).([]string)
	if len(pluginLoadDirs) > 0 {
		for _, plugDir := range pluginLoadDirs {
			dir, err := os.ReadDir(plugDir)
			if err != nil {
				o.logger.Error("failed to list plugin dir: %s", zap.Error(err))
				continue
			}
			for _, dirent := range dir {
				name := dirent.Name()
				if !strings.HasSuffix(name, ".so") {
					continue
				}
				path := plugDir
				if !strings.HasSuffix(path, "/") {
					path += "/"
				}
				if err := plugins.LoadAndStart(path+name, o); err != nil {
					o.logger.Error("!! failed to load plugin: %s\n", zap.Error(err))
				}
			}
		}
	}
}

func (o *Otter) P2P() host.Host {
	return o.p2p
}

func (o *Otter) DHT() *dualdht.DHT {
	return o.dht
}

func (o *Otter) HostID() peer.ID {
	return o.p2p.ID()
}

func (o *Otter) Stop() error {
	o.autotlsCM.Stop()
	o.logger.Debug("stopped AutoTLS")

	if err := o.mdns.Close(); err != nil {
		o.logger.Error("stopping mdns discovery service", zap.Error(err))
	}
	o.logger.Debug("stopped mDNS")

	if err := o.stopP2P(); err != nil {
		o.logger.Error("stopping p2p", zap.Error(err))
	}
	o.logger.Debug("stopped libp2p")

	if err := o.ds.Close(); err != nil {
		o.logger.Error("closing main datastore", zap.Error(err))
	}
	o.logger.Debug("stopped datastore")

	if err := o.rm.Close(); err != nil {
		o.logger.Error("closing resource manager", zap.Error(err))
	}
	o.logger.Debug("stopped resource manager")

	return nil
}

func (o *Otter) ensureSubsystems() {
	<-o.WaitForBootstrap(o.ctx)

	t := time.NewTicker(30 * time.Second)
	for range t.C {
		o.ensureSubsystemsForKeys()
	}
}

func (o *Otter) ensureSubsystemsForKeys() {
	logger := o.logger.Named("ensure")
	logger.Debug("ensuring subsystems")

	pks, err := o.Keys(o.ctx)
	if err != nil {
		logger.Error("failed to fetch keys to ensure subsystems", zap.Error(err))
	}

	for _, k := range pks {
		err = o.ensureSubsystemsForKey(k)
		if err != nil {
			logger.Error("ensuring subsystem for key", zap.String("key", string(k)), zap.Error(err))
		}
	}
}

func (o *Otter) ensureSubsystemsForKey(p id.PublicID) error {
	var errs error

	if _, err := o.GetOrNewAccountSyncer(o.ctx, p); err != nil {
		errors.Join(errs, err)
	}

	if _, err := o.getCollectorOrNew(p); err != nil {
		errors.Join(errs, err)
	}

	if _, err := o.GetOrNewDistributedStorageForKey(o.ctx, p); err != nil {
		errors.Join(errs, err)
	}

	peers, err := o.ResolveOtterNodesForKey(o.ctx, p)
	if err != nil {
		errors.Join(errs, err)
	}
	for _, p := range peers {
		if p == o.HostID() {
			continue
		}
		go func() {
			ctx, cancel := context.WithTimeout(o.ctx, 10*time.Second)
			defer cancel()

			err := o.p2p.Connect(ctx, peer.AddrInfo{ID: p})
			if err != nil {
				o.logger.Error("ensuring connection to key peers", zap.Error(err))
			}
		}()
	}

	return errs
}
