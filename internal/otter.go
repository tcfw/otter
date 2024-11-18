package internal

import (
	"context"
	"fmt"
	"os"
	"strings"

	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
)

type Otter struct {
	ctx context.Context
	p2p host.Host
	dht *dualdht.DHT
}

func (o *Otter) Crypto() otter.Cryptography { return o }
func (o *Otter) Protocols() otter.Protocols { return o }

func NewOtter(ctx context.Context) (*Otter, error) {
	o := &Otter{
		ctx: ctx,
	}

	if err := o.setupLibP2P(); err != nil {
		return nil, fmt.Errorf("initing libp2p: %w", err)
	}

	sub, err := o.p2p.EventBus().Subscribe(&event.EvtLocalProtocolsUpdated{})
	if err != nil {
		return nil, fmt.Errorf("getting sub for protocols: %w", err)
	}

	go func() {
		for evt := range sub.Out() {
			fmt.Printf("ℹ️ Protocols updated:\n\t%+t", evt)
		}
	}()

	pluginLoadDirs := o.GetConfigAs([]string{}, config.Plugins_LoadDir).([]string)
	if len(pluginLoadDirs) > 0 {
		for _, plugDir := range pluginLoadDirs {
			dir, err := os.ReadDir(plugDir)
			if err != nil {
				fmt.Printf("failed to list plugin dir: %s", err.Error())
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
					fmt.Printf("!! failed to load plugin: %s\n", err.Error())
				}
			}
		}
	}

	return o, nil
}

func (o *Otter) P2P() host.Host {
	return o.p2p
}

func (o *Otter) DHT() *dualdht.DHT {
	return o.dht
}

func (o *Otter) Stop() error {
	if err := o.stopP2P(); err != nil {
		return err
	}

	return nil
}
