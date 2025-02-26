package internal

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	ipfslite "github.com/hsanjuan/ipfs-lite"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/go-datastore"
	ipld "github.com/ipfs/go-ipld-format"
	p2pforge "github.com/ipshipyard/p2p-forge/client"
	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"go.uber.org/zap"
)

type Otter struct {
	ctx    context.Context
	logger *zap.Logger

	p2p    host.Host
	dht    *dualdht.DHT
	pubsub *pubsub.PubSub
	ipld   ipld.DAGService
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

	blocks := blockstore.NewGCBlockstore(blockstore.NewBlockstore(ds), blockstore.NewGCLocker())
	o.ipld, err = ipfslite.New(ctx, ds, blocks, o.p2p, o.dht, nil)
	if err != nil {
		return nil, fmt.Errorf("initing ipfs-lite: %w", err)
	}

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

	return o, nil
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

	if err := o.mdns.Close(); err != nil {
		o.logger.Error("stopping mdns discovery service", zap.Error(err))
	}

	if err := o.stopP2P(); err != nil {
		o.logger.Error("stopping p2p", zap.Error(err))
	}

	if err := o.ds.Close(); err != nil {
		o.logger.Error("closing main datastore", zap.Error(err))
	}

	if err := o.rm.Close(); err != nil {
		o.logger.Error("closing resource manager", zap.Error(err))
	}

	return nil
}
