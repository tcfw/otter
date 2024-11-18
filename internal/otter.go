package internal

import (
	"context"
	"fmt"

	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/tcfw/otter/pkg/otter"
)

type Otter struct {
	ctx context.Context
	p2p host.Host
	dht *dualdht.DHT
}

func (o *Otter) Crypto() otter.Cryptography { return o }

func NewOtter(ctx context.Context) (*Otter, error) {
	o := &Otter{
		ctx: ctx,
	}

	if err := o.setupLibP2P(); err != nil {
		return nil, fmt.Errorf("initing libp2p: %w", err)
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
