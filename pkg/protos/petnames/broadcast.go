package petnames

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/tcfw/otter/pkg/id"
	"go.uber.org/zap"
)

const (
	broadcastReprovideInternal = 10 * time.Minute
)

func (p *PetnamesHandler) Search(ctx context.Context, value string, limit int) ([]peer.AddrInfo, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	provs := make([]peer.AddrInfo, 0)

	for _, f := range allFields {
		wg.Add(1)

		go func() {
			defer wg.Done()

			pr, err := p.SearchField(ctx, f, value, limit)
			if err != nil {
				p.l.Error("looking up DHT value", zap.Error(err))
				return
			}

			mu.Lock()
			defer mu.Unlock()

			provs = append(provs, pr...)
		}()
	}

	wg.Wait()

	return provs, nil
}

func (p *PetnamesHandler) SearchField(ctx context.Context, field string, value string, limit int) ([]peer.AddrInfo, error) {
	dhtKey := p.formatKV(field, value)
	c, err := p.rawToCid([]byte(dhtKey))
	if err != nil {
		return nil, err
	}

	ctxTo, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	provs, err := p.o.DHTSearchProviders(ctxTo, c, limit)
	if err != nil {
		return nil, err
	}

	return provs, nil
}

func (p *PetnamesHandler) broadcast() {
	<-p.o.WaitForBootstrap(context.Background())

	if err := pnh.broadcastNames(); err != nil {
		pnh.l.Error("initially broadcasting names", zap.Error(err))
	}

	t := time.NewTicker(broadcastReprovideInternal)
	for range t.C {
		err := p.broadcastNames()
		if err != nil {
			p.l.Error("broadcasting names", zap.Error(err))
		}
	}
}

func (p *PetnamesHandler) broadcastNames() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	keys, err := p.o.Crypto().KeyStore().Keys(ctx)
	if err != nil {
		return err
	}

	for _, k := range keys {
		err = p.broadcastNamesForKey(ctx, k)
		if err != nil {
			p.l.Error("broadcasting for petname", zap.Any("key", k), zap.Error(err))
		}
	}

	return nil
}

func (p *PetnamesHandler) broadcastNamesForKey(ctx context.Context, k id.PublicID) error {
	sc, err := p.ForPublicID(k)
	if err != nil {
		return err
	}

	pn, err := sc.ProposedName()
	if err != nil {
		return err
	}

	if strings.Contains(pn, " ") {
		parts := strings.SplitN(pn, " ", 2)

		err = p.broadcastField(ctx, fieldFirstName, parts[0])
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		err = p.broadcastField(ctx, fieldLastName, parts[1])
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}

	err = p.broadcastField(ctx, fieldFullName, pn)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return nil
}

func (p *PetnamesHandler) broadcastField(ctx context.Context, key string, value string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	dhtKey := p.formatKV(key, value)

	c, err := p.rawToCid([]byte(dhtKey))
	if err != nil {
		return err
	}

	p.l.Debug("broadcasting petname field to DHT", zap.Any("key", key), zap.Any("value", value), zap.Any("cid", c.String()))

	return p.o.DHTProvide(ctx, c, true)
}

func (p *PetnamesHandler) formatKV(key, value string) string {
	return fmt.Sprintf("%s:%s:%s", dhtPrefix, key, strings.ToLower(value))
}

func (p *PetnamesHandler) rawToCid(d []byte) (cid.Cid, error) {
	mh, err := multihash.Sum(d, multihash.SHA2_256, multihash.DefaultLengths[multihash.SHA2_256])
	if err != nil {
		return cid.Undef, err
	}

	return cid.NewCidV1(uint64(multicodec.Raw), mh), nil
}
