package internal

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ipfs/go-datastore"
	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"go.uber.org/zap"
)

type Otter struct {
	ctx    context.Context
	p2p    host.Host
	dht    *dualdht.DHT
	logger *zap.Logger
	ui     *uiConnector
	ds     datastore.Datastore
}

func (o *Otter) Crypto() otter.Cryptography     { return o }
func (o *Otter) Protocols() otter.Protocols     { return o }
func (o *Otter) Logger(comp string) *zap.Logger { return o.logger.Named(comp) }
func (o *Otter) UI() otter.UI                   { return o.ui }

func NewOtter(ctx context.Context, logger *zap.Logger) (*Otter, error) {
	o := &Otter{
		ctx:    ctx,
		logger: logger,
	}

	ds, err := NewDatastoreStorage(o)
	if err != nil {
		return nil, fmt.Errorf("starting datastore: %w", err)
	}
	o.ds = ds

	if err := o.setupLibP2P(); err != nil {
		return nil, fmt.Errorf("initing libp2p: %w", err)
	}

	sub, err := o.p2p.EventBus().Subscribe(&event.EvtLocalProtocolsUpdated{})
	if err != nil {
		return nil, fmt.Errorf("getting sub for protocols: %w", err)
	}

	go func() {
		for evt := range sub.Out() {
			o.logger.Info("Protocols updated", zap.Any("protocols", evt))
		}
	}()

	if err := startBuiltinPlugins(o); err != nil {
		o.logger.Error("loading builtin plugins", zap.Error(err))
	}

	loadExternalPlugins(o)

	return o, nil
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
	if err := o.stopP2P(); err != nil {
		return err
	}

	if err := o.ds.Close(); err != nil {
		return err
	}

	return nil
}
