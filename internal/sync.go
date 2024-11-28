package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-datastore"
	crdt "github.com/ipfs/go-ds-crdt"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/tcfw/otter/pkg/id"
	"go.uber.org/zap"
)

const (
	syncerTopicPrefix = "/otter/sync/"
	syncInterval      = 30 * time.Second
)

var (
	accountSyncers   = map[id.PublicID]*syncer{}
	accountSyncersMu sync.RWMutex
)

type syncer struct {
	ctx    context.Context
	cancel func()

	publicSyncer  *crdt.Datastore
	privateSyncer *crdt.Datastore
}

func (s *syncer) Close() error {
	if err := s.publicSyncer.Close(); err != nil {
		return fmt.Errorf("closing public syncer: %w", err)
	}

	if err := s.privateSyncer.Close(); err != nil {
		return fmt.Errorf("closing private syncer: %w", err)
	}

	s.cancel()

	return nil
}

func (o *Otter) syncerPubSubFilter(pid peer.ID, topic string) bool {
	if !strings.HasPrefix(topic, syncerTopicPrefix) {
		return true
	}

	account := id.PublicID(strings.TrimPrefix(topic, syncerTopicPrefix))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	peers, err := o.getAllowedSyncerPeers(ctx, account)
	if err != nil {
		o.logger.Error("getting allows syncer peers: %w", zap.Error(err))
		return false
	}

	for _, peer := range peers {
		if peer == pid {
			return true
		}
	}

	return false
}

func (o *Otter) getAllowedSyncerPeers(ctx context.Context, pubk id.PublicID) ([]peer.ID, error) {
	sc, err := o.Storage().Public(pubk)
	if err != nil {
		return nil, fmt.Errorf("getting account public store: %w", err)
	}

	rawPeerList, err := sc.Get(ctx, "storagePeers")
	if err != nil {
		return nil, fmt.Errorf("getting storage peer allow list: %w", err)
	}

	peerList := []peer.ID{}
	if err := json.Unmarshal(rawPeerList, &peerList); err != nil {
		return nil, fmt.Errorf("decoding storage peer list: %w", err)
	}

	return peerList, nil
}

func (o *Otter) GetOrNewAccountSyncer(ctx context.Context, pubk id.PublicID) (*syncer, error) {
	accountSyncersMu.RLock()
	ds, ok := accountSyncers[pubk]
	accountSyncersMu.RUnlock()

	if !ok {
		nds, err := o.newAccountSyncer(ctx, pubk)
		if err != nil {
			return nil, fmt.Errorf("creating new account syncer: %w", err)
		}

		accountSyncersMu.Lock()
		defer accountSyncersMu.Unlock()

		accountSyncers[pubk] = nds
		return nds, nil
	}

	return ds, nil
}

func (o *Otter) newAccountSyncer(ctx context.Context, pubk id.PublicID) (*syncer, error) {
	sctx, cancel := context.WithCancel(ctx)

	topic := syncerTopicPrefix + string(pubk)

	bs, err := crdt.NewPubSubBroadcaster(sctx, o.pubsub, topic)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating syncer broadcaster: %w", err)
	}

	publicSyncer, err := crdt.New(o.ds, datastore.NewKey(publicKeyPrefix+string(pubk)), o.ipld, bs, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating public syncer: %w", err)
	}

	privateSyncer, err := crdt.New(o.ds, datastore.NewKey(privateKeyPrefix+string(pubk)), o.ipld, bs, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating private syncer: %w", err)
	}

	go func() {
		t := time.NewTicker(syncInterval)

		for {
			select {
			case <-sctx.Done():
				t.Stop()
				return
			case <-t.C:
				if err := publicSyncer.Sync(sctx, datastore.NewKey("/")); err != nil {
					o.logger.Error("syncing public syncer", zap.Error(err))
				}
				if err := privateSyncer.Sync(sctx, datastore.NewKey("/")); err != nil {
					o.logger.Error("syncing private syncer", zap.Error(err))
				}
			}
		}
	}()

	return &syncer{sctx, cancel, publicSyncer, privateSyncer}, nil
}

func (o *Otter) StopAccountSyncer(ctx context.Context, pubk id.PublicID) error {
	ds, ok := accountSyncers[pubk]
	if !ok {
		return errors.New("account syncer not found")
	}

	accountSyncersMu.Lock()
	defer accountSyncersMu.Unlock()

	delete(accountSyncers, pubk)
	if err := ds.Close(); err != nil {
		return fmt.Errorf("closing syncer datastore: %w", err)
	}

	return nil
}
