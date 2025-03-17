package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	v1api "github.com/tcfw/otter/pkg/api"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"

	"github.com/ipfs/go-datastore"
	crdt "github.com/ipfs/go-ds-crdt"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

const (
	syncerTopicPrefix = otterTopicPrefix + "sync/"
	syncInterval      = 30 * time.Second
)

var (
	accountSyncers   = map[id.PublicID]*syncer{}
	accountSyncersMu sync.RWMutex
)

type syncer struct {
	ctx           context.Context
	cancel        chan struct{}
	logger        *zap.Logger
	publicSyncer  *crdt.Datastore
	privateSyncer *crdt.Datastore

	hookMu      sync.RWMutex
	putHooks    []otter.PutHook
	deleteHooks []otter.DeleteHook
}

func (s *syncer) Close() error {
	if err := s.publicSyncer.Close(); err != nil {
		return fmt.Errorf("closing public syncer: %w", err)
	}

	if err := s.privateSyncer.Close(); err != nil {
		return fmt.Errorf("closing private syncer: %w", err)
	}

	close(s.cancel)

	return nil
}

func (s *syncer) AddPutHook(f otter.PutHook) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()

	s.putHooks = append(s.putHooks, f)
}

func (s *syncer) AddDeleteHook(f otter.DeleteHook) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()

	s.deleteHooks = append(s.deleteHooks, f)
}

func (s *syncer) putHook(k datastore.Key, v []byte) {
	s.hookMu.RLock()
	defer s.hookMu.RUnlock()

	for _, hook := range s.putHooks {
		hook(k, v)
	}
}

func (s *syncer) deleteHook(k datastore.Key) {
	s.hookMu.RLock()
	defer s.hookMu.RUnlock()

	for _, hook := range s.deleteHooks {
		hook(k)
	}
}

func (o *Otter) syncerPubSubFilter(pid peer.ID, topic string) bool {
	if pid == o.HostID() {
		return true
	}

	<-o.WaitForBootstrap(o.ctx)

	logger := o.logger.Named("sync.pubsub-filter")
	logger.Debug("checking peer", zap.Any("topic", topic), zap.Any("peer", pid.String()))

	if !strings.HasPrefix(topic, syncerTopicPrefix) {
		logger.Debug("skipping topic validation, unexpected prefix", zap.Any("topic", topic), zap.Any("peer", pid.String()))
		return false
	}

	if pid == o.HostID() {
		logger.Debug("skipping self", zap.Any("topic", topic), zap.Any("peer", pid.String()))
		return false
	}

	pst := strings.TrimPrefix(topic, syncerTopicPrefix)
	pst = strings.TrimSuffix(pst, ".priv")
	pst = strings.TrimSuffix(pst, ".pub")
	account := id.PublicID(pst)

	ctx, cancel := context.WithTimeout(o.ctx, 5*time.Second)
	defer cancel()

	if !accountSyncersMu.TryRLock() {
		//possibly trying to start the publisher for storage
		logger.Debug("skipping peer validation, account syncer is locked maybe", zap.Any("topic", topic), zap.Any("peer", pid.String()))
		return false
	}
	accountSyncersMu.RUnlock()

	peers, err := o.getAllowedSyncerPeers(ctx, account)
	if err != nil {
		logger.Error("getting allows syncer peers: %w", zap.Error(err))
		return false
	}

	if len(peers) == 0 {
		//TODO(tcfw): bootstrap allowed peers somehow
		return false
	}

	for _, peer := range peers {
		if peer == pid {
			logger.Debug("peer allowed", zap.Any("remote", pid.String()), zap.Any("allowed", peer.String()))
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

	rawPeerList, err := sc.Get(ctx, datastore.NewKey("storagePeers"))
	if err != nil && !errors.Is(err, datastore.ErrNotFound) {
		return nil, fmt.Errorf("getting storage peer allow list: %w", err)
	}

	if len(rawPeerList) == 0 {
		rawPeerList = []byte(`[]`)
	}

	peerList := []peer.ID{}
	if err := json.Unmarshal(rawPeerList, &peerList); err != nil {
		return nil, fmt.Errorf("decoding storage peer list: %w", err)
	}

	return peerList, nil
}

func (o *Otter) setAllowedSyncerPeers(ctx context.Context, pubk id.PublicID, peers []peer.ID) error {
	sc, err := o.Storage().Public(pubk)
	if err != nil {
		return fmt.Errorf("getting account public store: %w", err)
	}

	buf, err := json.Marshal(peers)
	if err != nil {
		return fmt.Errorf("encoding storage peer list: %w", err)
	}

	err = sc.Put(ctx, datastore.NewKey("storagePeers"), buf)
	if err != nil {
		return fmt.Errorf("getting storage peer allow list: %w", err)
	}

	return nil
}

func (o *Otter) GetOrNewAccountSyncer(ctx context.Context, pubk id.PublicID) (*syncer, error) {
	accountSyncersMu.Lock()
	defer accountSyncersMu.Unlock()

	ds, ok := accountSyncers[pubk]
	if ok {
		return ds, nil
	}

	nds, err := o.newAccountSyncer(ctx, pubk)
	if err != nil {
		return nil, fmt.Errorf("creating new account syncer: %w", err)
	}

	accountSyncers[pubk] = nds
	return nds, nil
}

func (o *Otter) newAccountSyncer(ctx context.Context, pubk id.PublicID) (*syncer, error) {
	canCh := make(chan struct{})

	s := &syncer{ctx: ctx, cancel: canCh, logger: o.logger.Named("syncer." + string(pubk))}

	opts := crdt.DefaultOptions()
	opts.Logger = s.logger.Named("crdt").Sugar()
	opts.PutHook = s.putHook
	opts.DeleteHook = s.deleteHook

	publicPrefix := datastore.NewKey(publicKeyPrefix + string(pubk))
	privatePrefix := datastore.NewKey(privateKeyPrefix + string(pubk))

	publicTopic := syncerTopicPrefix + string(pubk) + ".pub"

	pubBroadcaster, err := crdt.NewPubSubBroadcaster(ctx, o.pubsub, publicTopic)
	if err != nil {
		return nil, fmt.Errorf("creating syncer broadcaster: %w", err)
	}

	privTopic := syncerTopicPrefix + string(pubk) + ".priv"

	privBroadcaster, err := crdt.NewPubSubBroadcaster(ctx, o.pubsub, privTopic)
	if err != nil {
		return nil, fmt.Errorf("creating syncer broadcaster: %w", err)
	}

	s.publicSyncer, err = crdt.New(o.ds, publicPrefix, o.ipld, pubBroadcaster, opts)
	if err != nil {
		return nil, fmt.Errorf("creating public syncer: %w", err)
	}

	s.privateSyncer, err = crdt.New(o.ds, privatePrefix, o.ipld, privBroadcaster, opts)
	if err != nil {
		return nil, fmt.Errorf("creating private syncer: %w", err)
	}

	go func() {
		t := time.NewTicker(syncInterval)

		for {
			select {
			case <-canCh:
				t.Stop()
				return
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
				if err := s.publicSyncer.Sync(ctx, datastore.NewKey("/")); err != nil {
					o.logger.Error("syncing public syncer", zap.Error(err))
				}
				if err := s.privateSyncer.Sync(ctx, datastore.NewKey("/")); err != nil {
					o.logger.Error("syncing private syncer", zap.Error(err))
				}
			}
		}
	}()

	return s, nil
}

func (o *Otter) StopAccountSyncer(ctx context.Context, pubk id.PublicID) error {
	accountSyncersMu.RLock()
	ds, ok := accountSyncers[pubk]
	accountSyncersMu.RUnlock()

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

func (o *Otter) apiHandle_Sync_GetAllowedPeers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	peers, err := o.getAllowedSyncerPeers(ctx, id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(peers); err != nil {
		apiJSONError(w, err)
		return
	}
}

func (o *Otter) apiHandle_Sync_SetAllowedPeers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	peers := []peer.ID{}

	if err := json.NewDecoder(io.LimitReader(r.Body, 10*1024)).Decode(&peers); err != nil {
		apiJSONError(w, err)
		return
	}

	if len(peers) == 0 {
		apiJSONErrorWithStatus(w, errors.New("at least 1 peer is required"), http.StatusBadRequest)
		return
	}

	for _, peer := range peers {
		if err := peer.Validate(); err != nil {
			apiJSONErrorWithStatus(w, err, http.StatusBadRequest)
			return
		}
	}

	err = o.setAllowedSyncerPeers(ctx, id, peers)
	if err != nil {
		apiJSONError(w, err)
		return
	}
}

func (o *Otter) apiHandle_Sync_Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	s, err := o.GetOrNewAccountSyncer(o.ctx, id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	resp := &v1api.SyncStatsResponse{
		Public:  s.publicSyncer.InternalStats(ctx),
		Private: s.privateSyncer.InternalStats(ctx),
	}

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
