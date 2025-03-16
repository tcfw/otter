package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	ipfslite "github.com/hsanjuan/ipfs-lite"
	chunker "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	"github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	dsBadger3 "github.com/ipfs/go-ds-badger3"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/jbenet/goprocess"
	"github.com/libp2p/go-libp2p/core/peer"
	multihash "github.com/multiformats/go-multihash/core"
	"github.com/tcfw/otter/internal/storage"
	v1api "github.com/tcfw/otter/pkg/api"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/otter/pb"
	"go.uber.org/zap"
	protobuf "google.golang.org/protobuf/proto"
)

const (
	systemKeyPrefix  = "/system/"
	publicKeyPrefix  = "/account/public/"
	privateKeyPrefix = "/account/private/"

	systemPrefix_Keys = "/keys/"
	systemPrefix_Pass = "pass/"

	distStorageCheckInterval = 5 * time.Minute
)

var (
	distStorageGlobalJobQueue = make(chan *distStorageJob, 1000)

	distStorageSyncers   = map[id.PublicID]*distributedStorage{}
	distStorageSyncersMu sync.RWMutex

	distStorageJobDefaultMaxRetries = 5
)

// NewDiskDatastoreStorage instatiates a Badger3 datastore for perminant on-disk
// storage
func NewDiskDatastoreStorage(o *Otter) (datastore.Batching, error) {
	dataDir := o.GetConfig(config.Storage_Dir).(string)
	if _, err := os.Stat(dataDir); err == os.ErrNotExist {
		if err := os.MkdirAll(dataDir, os.FileMode(0600)); err != nil {
			return nil, fmt.Errorf("making data dir: %w", err)
		}
	}

	dk, err := o.DiskKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get disk key: %w", err)
	}

	options := dsBadger3.DefaultOptions
	options.EncryptionKey = dk
	options.IndexCacheSize = 20480

	ds, err := dsBadger3.NewDatastore(dataDir, &options)
	if err != nil {
		return nil, fmt.Errorf("initing datastore: %w", err)
	}

	return ds, nil
}

func (o *Otter) apiHandle_Storage_ListKeys(w http.ResponseWriter, r *http.Request) {
	res, err := o.ds.Query(r.Context(), query.Query{KeysOnly: true})
	if err != nil {
		apiJSONError(w, err)
		return
	}

	keys, err := res.Rest()
	if err != nil {
		apiJSONError(w, err)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

type cryptoSealUnsealer func(ctx context.Context, b []byte) ([]byte, error)

type StorageClasses struct {
	o *Otter
}

// Public provides a storage class for a given public key
// No values or keys are encrypted and objects are assumed to *not* be globally shareable
func (sc *StorageClasses) System() (datastore.Datastore, error) {
	return &NamespacedStorage{
		Datastore: sc.o.ds,
		logger:    sc.o.logger.Named("system_storage"),
		ns:        datastore.NewKey(systemKeyPrefix),
	}, nil
}

// Public provides a storage class for a given public key
// No values or keys are encrypted and objects are assumed to be globally shareable
func (sc *StorageClasses) Public(pub id.PublicID) (datastore.Datastore, error) {
	syncer, err := sc.o.GetOrNewAccountSyncer(sc.o.ctx, pub)
	if err != nil {
		return nil, fmt.Errorf("getting account syncer: %w", err)
	}

	return &NamespacedStorage{
		Datastore:  syncer.publicSyncer,
		logger:     sc.o.logger.Named("public_storage"),
		ns:         datastore.NewKey(publicKeyPrefix + string(pub)),
		putHook:    syncer.AddPutHook,
		deleteHook: syncer.AddDeleteHook,
	}, nil
}

// Private provides a storage class for a given private key
// All values will be sealed using the derrived storage key
// Keys are not encrypted
//
// Values are assumed to be shareable once sealed
func (sc *StorageClasses) Private(pk id.PrivateKey) (datastore.Datastore, error) {
	pubk, err := pk.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("getting public key: %w", err)
	}

	syncer, err := sc.o.GetOrNewAccountSyncer(sc.o.ctx, pubk)
	if err != nil {
		return nil, fmt.Errorf("getting account syncer: %w", err)
	}

	sk, err := privateKeytoStorageKey(pk, nil)
	if err != nil {
		return nil, fmt.Errorf("getting storage key: %w", err)
	}

	aead, err := privateStorageAEAD(sk)
	if err != nil {
		return nil, fmt.Errorf("creating AEAD: %w", err)
	}

	ad := []byte(pubk)

	ns := &NamespacedStorage{
		Datastore:  syncer.privateSyncer,
		logger:     sc.o.logger.Named("private_storage"),
		ns:         datastore.NewKey(privateKeyPrefix + string(pubk)),
		seal:       privateStorageSeal(aead, ad),
		unseal:     privateStorageUnseal(aead, ad),
		putHook:    syncer.AddPutHook,
		deleteHook: syncer.AddDeleteHook,
	}

	return ns, nil
}

// NamespacedStorage provides namespaced and/or encryption functions for a datastore
type NamespacedStorage struct {
	datastore.Datastore

	ns     datastore.Key
	logger *zap.Logger

	seal   cryptoSealUnsealer
	unseal cryptoSealUnsealer

	putHook    func(syncerPutHook)
	deleteHook func(syncerDeleteHook)
}

func trimNamespacePrefix(prefix datastore.Key, k datastore.Key) datastore.Key {
	return datastore.NewKey(strings.TrimPrefix(k.String(), prefix.String()))
}

func (nss *NamespacedStorage) PutHook(f syncerPutHook) {
	nss.putHook(func(k datastore.Key, b []byte) {
		if k.IsDescendantOf(nss.ns) {
			f(trimNamespacePrefix(nss.ns, k), b)
		}
	})
}

func (nss *NamespacedStorage) DeleteHook(f syncerDeleteHook) {
	nss.deleteHook(func(k datastore.Key) {
		if k.IsDescendantOf(nss.ns) {
			f(trimNamespacePrefix(nss.ns, k))
		}
	})
}

// formatKey wraps the key with the storage class namespace
func (nss *NamespacedStorage) formatKey(k datastore.Key) datastore.Key {
	return nss.ns.Child(k)
}

// Get retreives a value for the given value from the datastore
// unsealing the value if configured
func (nss *NamespacedStorage) Get(ctx context.Context, k datastore.Key) ([]byte, error) {
	val, err := nss.Datastore.Get(ctx, nss.formatKey(k))
	if err != nil {
		return nil, err
	}

	if nss.unseal != nil {
		unsealedVal, err := nss.unseal(ctx, val)
		if err != nil {
			return nil, fmt.Errorf("unsealing value: %w", err)
		}
		return unsealedVal, nil
	}

	return val, nil
}

// Has returns whether the `key` is mapped to a `value`.
func (nss *NamespacedStorage) Has(ctx context.Context, k datastore.Key) (bool, error) {
	return nss.Datastore.Has(ctx, nss.formatKey(k))
}

// Put adds a key/value to the datastore
// sealing the value if configured
func (nss *NamespacedStorage) Put(ctx context.Context, k datastore.Key, val []byte) error {
	if nss.seal != nil {
		sealedVal, err := nss.seal(ctx, val)
		if err != nil {
			return fmt.Errorf("sealing value: %w", err)
		}
		val = sealedVal
	}

	return nss.Datastore.Put(ctx, nss.formatKey(k), val)
}

// Search finds keys in the datastore with the prefix
func (nss *NamespacedStorage) Query(ctx context.Context, q query.Query) (query.Results, error) {
	q.Prefix = nss.formatKey(datastore.NewKey(q.Prefix)).String()

	r, err := nss.Datastore.Query(ctx, q)
	if err != nil {
		return nil, err
	}

	return &NamespacedQueryResults{ctx, nss, q, r}, nil
}

func (nss *NamespacedStorage) Close() error {
	return nil
}

// Delete removes the given key from the datastore
func (nss *NamespacedStorage) Delete(ctx context.Context, k datastore.Key) error {
	return nss.Datastore.Delete(ctx, nss.formatKey(k))
}

type NamespacedQueryResults struct {
	ctx context.Context
	nss *NamespacedStorage
	q   query.Query
	r   query.Results
}

func (nsr *NamespacedQueryResults) Query() query.Query {
	return nsr.q
}

func (nsr *NamespacedQueryResults) Next() <-chan query.Result {
	ch := make(chan query.Result)

	go func() {
		defer close(ch)

		for r := range nsr.r.Next() {
			if nsr.nss.unseal != nil {
				v, err := nsr.nss.unseal(nsr.ctx, r.Value)
				if err != nil {
					r.Error = err
				} else {
					r.Value = v
				}
			}
			r.Key = strings.TrimPrefix(r.Key, nsr.nss.ns.String())
			ch <- r
		}
	}()

	return ch
}

func (nsr *NamespacedQueryResults) NextSync() (query.Result, bool) {
	val, ok := nsr.r.NextSync()

	if nsr.nss.unseal != nil {
		v, err := nsr.nss.unseal(nsr.ctx, val.Value)
		if err != nil {
			val.Error = err
			return val, false
		} else {
			val.Value = v
		}
	}

	val.Key = strings.TrimPrefix(val.Key, nsr.nss.ns.String())

	return val, ok
}

func (nsr *NamespacedQueryResults) Rest() ([]query.Entry, error) {
	var es []query.Entry

	for r := range nsr.r.Next() {
		if nsr.nss.unseal != nil {
			v, err := nsr.nss.unseal(nsr.ctx, r.Value)
			if err != nil {
				return nil, err
			}
			r.Value = v
		}

		r.Key = strings.TrimPrefix(r.Key, nsr.nss.ns.String())
		es = append(es, r.Entry)
	}

	return es, nil
}

func (nsr *NamespacedQueryResults) Close() error {
	return nsr.r.Close()
}

func (nsr *NamespacedQueryResults) Process() goprocess.Process {
	return nsr.r.Process()
}

func (o *Otter) apiHandle_DistStorage_PinInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	infoKey, ok := mux.Vars(r)["info"]
	if !ok {
		apiJSONErrorWithStatus(w, errors.New("missing info"), http.StatusBadRequest)
		return
	}

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	ds, err := o.GetOrNewDistributedStorageForKey(ctx, id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	k := datastore.KeyWithNamespaces([]string{cidPinPrefix, infoKey})

	b, err := ds.pinManagement.Get(ctx, k)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	info := &pb.PinInfo{}
	err = protobuf.Unmarshal(b, info)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (o *Otter) apiHandle_DistStorage_Metrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	metrics, err := o.getCollectorOrNew(id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	metrics.lastMu.RLock()
	defer metrics.lastMu.RUnlock()

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics.last)
}

func (o *Otter) apiHandle_DistStorage_ListPins(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	ds, err := o.GetOrNewDistributedStorageForKey(ctx, id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	q, err := ds.pinManagement.Query(ctx, query.Query{})
	if err != nil {
		apiJSONError(w, err)
		return
	}

	res, err := q.Rest()
	if err != nil {
		apiJSONError(w, err)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (o *Otter) apiHandle_DistStorage_Add(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	ds, err := o.GetOrNewDistributedStorageForKey(ctx, id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	cid, err := ds.AddFromReader(ctx, r.Body, otter.WithEncrypted(), otter.WithMinReplicas(1), otter.WithMaxReplicas(2))
	if err != nil {
		apiJSONError(w, err)
		return
	}

	w.Write([]byte(cid.String()))
}

func (o *Otter) apiHandle_DistStorage_Remove(w http.ResponseWriter, r *http.Request) {
	cs := mux.Vars(r)["cid"]
	if cs == "" {
		apiJSONErrorWithStatus(w, fmt.Errorf("cid required"), http.StatusBadRequest)
		return
	}

	c, err := cid.Decode(cs)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	ds, err := o.GetOrNewDistributedStorageForKey(ctx, id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	err = ds.Remove(ctx, c)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	w.Write([]byte("ok"))
}

func (o *Otter) apiHandle_DistStorage_Get(w http.ResponseWriter, r *http.Request) {
	cs := mux.Vars(r)["cid"]
	if cs == "" {
		apiJSONErrorWithStatus(w, fmt.Errorf("cid required"), http.StatusBadRequest)
		return
	}

	c, err := cid.Decode(cs)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	ctx := r.Context()

	id, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	ds, err := o.GetOrNewDistributedStorageForKey(ctx, id)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	var d io.ReadCloser

	if r.URL.Query().Has("encrypted") {
		d, err = ds.GetEncrypted(ctx, c)
	} else {
		d, err = ds.Get(ctx, c)
	}

	if err != nil {
		apiJSONError(w, err)
		return
	}
	defer d.Close()

	if _, err := io.Copy(w, d); err != nil {
		apiJSONError(w, err)
		return
	}
}

func (o *Otter) GetOrNewDistributedStorageForKey(ctx context.Context, pub id.PublicID) (*distributedStorage, error) {
	distStorageSyncersMu.RLock()
	ds, ok := distStorageSyncers[pub]
	distStorageSyncersMu.RUnlock()

	if ok {
		return ds, nil
	}

	distStorageSyncersMu.Lock()
	defer distStorageSyncersMu.Unlock()

	sync, err := o.GetOrNewAccountSyncer(ctx, pub)
	if err != nil {
		return nil, err
	}

	logger := o.logger.Named("dist_storage")

	pinDS := &NamespacedStorage{
		Datastore:  sync.privateSyncer,
		ns:         datastore.NewKey("pins"),
		logger:     logger,
		putHook:    sync.AddPutHook,
		deleteHook: sync.AddDeleteHook,
	}

	metricsCollector, err := o.getCollectorOrNew(pub)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(o.ctx)

	ds = &distributedStorage{
		ctx:           ctx,
		pkGetter:      o.getPK,
		metrics:       metricsCollector,
		pubID:         pub,
		stop:          cancel,
		nodeId:        o.HostID().String(),
		logger:        logger,
		pinManagement: pinDS,

		ipfs:     o.ipld.(*ipfslite.Peer),
		jobQueue: distStorageGlobalJobQueue,
		xfers:    make(map[cid.Cid]struct{}),
	}

	pinDS.PutHook(func(k datastore.Key, _ []byte) { ds.hintAdd(k) })
	pinDS.DeleteHook(func(k datastore.Key) { ds.hintRemove(k) })

	go ds.startWatch()

	//start workers to scale the workers per keys active
	for range 2 {
		go ds.worker()
	}

	distStorageSyncers[pub] = ds

	return ds, nil

}

type distStorageJobAction int

const (
	distStorageJob_Add distStorageJobAction = iota + 1
	distStorageJob_Remove
)

type distStorageJob struct {
	cid      cid.Cid
	action   distStorageJobAction
	indirect bool

	wait chan struct{}

	tries    int
	maxTries int
}

const (
	nodePinPrefix = "n"
	cidPinPrefix  = "p"
)

type distributedStorage struct {
	ctx      context.Context
	stop     func()
	pkGetter func(ctx context.Context, p id.PublicID) (id.PrivateKey, error)
	nodeId   string
	logger   *zap.Logger

	pubID         id.PublicID
	pinManagement datastore.Datastore
	ipfs          *ipfslite.Peer
	metrics       *Collector

	checkMu sync.Mutex

	jobQueue chan *distStorageJob

	xferMu sync.RWMutex
	xfers  map[cid.Cid]struct{}
}

func (ds *distributedStorage) startWatch() {
	t := time.NewTicker(1)

	for {
		select {
		case <-ds.ctx.Done():
			return
		case <-t.C:
			err := ds.checkShouldBePinned()
			if err != nil {
				ds.logger.Error("checking pins", zap.Error(err))
			}

			t.Reset(distStorageCheckInterval)
		}
	}
}

func (ds *distributedStorage) hintAdd(v datastore.Key) {
	if !v.IsDescendantOf(datastore.KeyWithNamespaces([]string{nodePinPrefix, ds.nodeId})) {
		return
	}

	cid, err := cid.Decode(v.BaseNamespace())
	if err != nil {
		ds.logger.Error("unable to cast CID for add hint", zap.Any("cid", v))
		return
	}

	ds.logger.Debug("got new hint add", zap.Any("cid", cid))

	infoBytes, err := ds.pinManagement.Get(ds.ctx, v)
	if err != nil && !errors.Is(err, datastore.ErrNotFound) {
		ds.logger.Error("unable to fetch key for add hint", zap.Any("cid", v))
		return
	}

	if len(infoBytes) > 0 {
		info := &pb.PinState{}
		if err := protobuf.Unmarshal(infoBytes, info); err != nil {
			ds.logger.Error("unable to decode key for add hint", zap.Any("cid", v))
			return
		}

		if info.Pinned {
			ds.logger.Debug("skipping hint add, already pinned", zap.Any("cid", cid))
			return
		}
	}

	ds.logger.Debug("queueing add", zap.Any("cid", cid))

	ds.jobQueue <- &distStorageJob{
		action: distStorageJob_Add,
		cid:    cid,
	}
}

func (ds *distributedStorage) hintRemove(v datastore.Key) {
	if !v.IsDescendantOf(datastore.NewKey(cidPinPrefix)) &&
		!v.IsDescendantOf(datastore.KeyWithNamespaces([]string{nodePinPrefix, ds.nodeId})) {
		return
	}

	cid, err := cid.Decode(v.BaseNamespace())
	if err != nil {
		ds.logger.Error("unable to cast CID for removal hint", zap.Any("cid", v))
		return
	}

	ds.jobQueue <- &distStorageJob{
		action: distStorageJob_Remove,
		cid:    cid,
	}
}

func (ds *distributedStorage) checkShouldBePinned() error {
	if !ds.checkMu.TryLock() {
		return errors.New("check already running")
	}
	defer ds.checkMu.Unlock()

	ctx, cancel := context.WithCancel(ds.ctx)
	defer cancel()

	prefix := datastore.KeyWithNamespaces([]string{nodePinPrefix, ds.nodeId})

	res, err := ds.pinManagement.Query(ctx, query.Query{Prefix: prefix.String()})
	if err != nil {
		return err
	}

	for pin := range res.Next() {
		cidStr := datastore.NewKey(pin.Key).BaseNamespace()
		cid, err := cid.Decode(cidStr)
		if err != nil {
			ds.logger.Error("reading CID from node pinset", zap.Error(err), zap.Any("cid", cidStr))
			continue
		}

		exists, err := ds.ipfs.HasBlock(ctx, cid)
		if err != nil {
			ds.logger.Error("checking CID in local pinset", zap.Error(err), zap.Any("cid", cidStr))
			continue
		}

		info := &pb.PinState{}
		if err := protobuf.Unmarshal(pin.Value, info); err != nil {
			ds.logger.Error("decoding pininfo", zap.Error(err), zap.Any("cid", cidStr))
			continue
		}

		if !exists || !info.Pinned {
			ds.logger.Debug("suspected missing block", zap.String("cid", cid.String()))
			ds.xferMu.RLock()
			if _, ok := ds.xfers[cid]; !ok {
				ds.jobQueue <- &distStorageJob{
					action:   distStorageJob_Add,
					cid:      cid,
					indirect: info.Indirect,
				}
			}
			ds.xferMu.RUnlock()

			continue
		}

		//TODO(check indrect pins)
	}

	return nil
}

func (ds *distributedStorage) worker() {
	defer func() {
		ds.logger.Warn("dist_storage worker ended")
	}()

	for {
		select {
		case <-ds.ctx.Done():
			return
		case job, ok := <-ds.jobQueue:
			if !ok {
				return
			}

			if job.maxTries == 0 {
				job.maxTries = distStorageJobDefaultMaxRetries
			}

			err := ds.doJob(job)
			if err != nil {
				ds.logger.Error("error processing job", zap.Error(err))

				job.tries++
				if job.tries <= job.maxTries {
					ds.jobQueue <- job
				} else {
					ds.logger.Debug("job tries exceeded", zap.Any("cid", job.cid))
				}
			}
		}
	}
}

func (ds *distributedStorage) doJob(job *distStorageJob) error {
	var err error

	switch job.action {
	case distStorageJob_Add:
		err = ds.doAdd(job.cid)
	case distStorageJob_Remove:
		err = ds.doRemove(job.cid)
	default:
		return errors.New("unknown job type")
	}
	if err != nil {
		return err
	}

	if job.wait != nil {
		close(job.wait)
	}

	return nil
}

func (ds *distributedStorage) doAdd(c cid.Cid) error {
	ds.xferMu.RLock()
	_, ok := ds.xfers[c]
	ds.xferMu.RUnlock()
	if ok {
		ds.logger.Debug("ignoring add, already in progress", zap.Any("cid", c.String()))
		return nil
	}

	ds.logger.Debug("adding CID to local blocks", zap.Any("cid", c.String()))

	ds.xferMu.Lock()
	ds.xfers[c] = struct{}{}
	ds.xferMu.Unlock()

	k := datastore.KeyWithNamespaces([]string{nodePinPrefix, ds.nodeId, c.String()})

	psExists, err := ds.pinManagement.Get(ds.ctx, k)
	if err != nil && !errors.Is(err, datastore.ErrNotFound) {
		return fmt.Errorf("getting pinset key for pinInfo")
	}

	hasBlock, err := ds.ipfs.HasBlock(ds.ctx, c)
	if err != nil {
		return err
	}

	//skip if we already have it, we might have the block but not updated the pinState
	//if the event is due to an indirect add it should be set already
	if len(psExists) != 0 && hasBlock {
		return nil
	}

	ds.logger.Debug("adding to pin", zap.Any("cid", c.String()))

	defer func() {
		ds.xferMu.Lock()
		defer ds.xferMu.Unlock()

		delete(ds.xfers, c)
	}()

	node, err := ds.ipfs.Get(ds.ctx, c)
	if err != nil {
		return err
	}

	if err := ds.ipfs.Add(ds.ctx, node); err != nil {
		return err
	}

	nav := ipld.NewWalker(ds.ctx, ipld.NewNavigableIPLDNode(node, ds.ipfs))
	err = nav.Iterate(func(node ipld.NavigableNode) error {
		inode := node.GetIPLDNode()

		if inode.Cid() == c {
			return nil
		}

		ds.logger.Debug("adding indirect CID to local blocks", zap.Any("cid", inode.Cid().String()))

		if err := ds.ipfs.Add(ds.ctx, inode); err != nil {
			return err
		}

		k := datastore.KeyWithNamespaces([]string{nodePinPrefix, ds.nodeId, inode.Cid().String()})

		info := &pb.PinState{
			Indirect: true,
			Pinned:   true,
			Parent:   c.String(),
		}

		vb, err := protobuf.Marshal(info)
		if err != nil {
			return err
		}

		return ds.pinManagement.Put(ds.ctx, k, vb)
	})
	if err != nil && !errors.Is(err, ipld.EndOfDag) {
		return err
	}

	info := &pb.PinState{
		Pinned: true,
	}

	vb, err := protobuf.Marshal(info)
	if err != nil {
		return err
	}

	return ds.pinManagement.Put(ds.ctx, k, vb)
}

func (ds *distributedStorage) doRemove(c cid.Cid) error {
	ok, err := ds.ipfs.HasBlock(ds.ctx, c)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	k := datastore.KeyWithNamespaces([]string{nodePinPrefix, ds.nodeId, c.String()})

	node, err := ds.ipfs.DAGService.Get(ds.ctx, c)
	if err != nil {
		return err
	}

	err = ds.ipfs.BlockStore().DeleteBlock(ds.ctx, c)
	if err != nil {
		return err
	}

	if err := ds.pinManagement.Delete(ds.ctx, k); err != nil {
		return err
	}

	nav := ipld.NewWalker(ds.ctx, ipld.NewNavigableIPLDNode(node, ds.ipfs))
	nav.Iterate(func(node ipld.NavigableNode) error {
		inode := node.GetIPLDNode()

		if err := ds.ipfs.BlockStore().DeleteBlock(ds.ctx, c); err != nil {
			return err
		}

		k := datastore.KeyWithNamespaces([]string{nodePinPrefix, ds.nodeId, inode.Cid().String()})

		return ds.pinManagement.Delete(ds.ctx, k)
	})

	return nil
}

func (ds *distributedStorage) Info(ctx context.Context, c cid.Cid) (*pb.PinInfo, error) {
	k := datastore.KeyWithNamespaces([]string{cidPinPrefix, c.String()})
	v, err := ds.pinManagement.Get(ctx, k)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			return nil, otter.ErrNotFound
		}

		return nil, err
	}

	pi := &pb.PinInfo{}
	err = protobuf.Unmarshal(v, pi)
	if err != nil {
		return nil, err
	}

	return pi, nil
}

func (ds *distributedStorage) Get(ctx context.Context, c cid.Cid) (io.ReadSeekCloser, error) {
	r, err := ds.ipfs.GetFile(ctx, c)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (ds *distributedStorage) GetEncrypted(ctx context.Context, c cid.Cid) (io.ReadCloser, error) {
	n, err := ds.ipfs.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	pk, err := ds.pkGetter(ctx, ds.pubID)
	if err != nil {
		return nil, err
	}

	pubk, err := pk.PublicKey()
	if err != nil {
		return nil, err
	}

	ad := []byte(pubk)

	sk, err := privateKeytoStorageKey(pk, nil)
	if err != nil {
		return nil, fmt.Errorf("getting storage key: %w", err)
	}

	aead, err := privateStorageAEAD(sk)
	if err != nil {
		return nil, fmt.Errorf("creating AEAD: %w", err)
	}

	return storage.NewEncryptedUnixFSReader(ctx, n, ds.ipfs, aead, ad)
}

func (ds *distributedStorage) Remove(ctx context.Context, c cid.Cid) error {
	k := datastore.KeyWithNamespaces([]string{cidPinPrefix, c.String()})
	ok, err := ds.pinManagement.Has(ctx, k)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			return otter.ErrNotFound
		}

		return err
	}
	if !ok {
		return otter.ErrNotFound
	}

	return ds.pinManagement.Delete(ctx, k)
}

func (ds *distributedStorage) defaultConfig() *pb.AddConfig {
	return &pb.AddConfig{}
}

func (ds *distributedStorage) AddFromSlice(ctx context.Context, b []byte, opts ...otter.AddOption) (cid.Cid, error) {
	return ds.AddFromReader(ctx, bytes.NewReader(b), opts...)
}

func (ds *distributedStorage) AddFromReader(ctx context.Context, r io.Reader, opts ...otter.AddOption) (cid.Cid, error) {
	cfg := ds.defaultConfig()
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return cid.Undef, err
		}
	}

	prefix, err := merkledag.PrefixForCidVersion(1)
	if err != nil {
		return cid.Undef, fmt.Errorf("bad CID Version: %s", err)
	}

	prefix.MhType = multihash.SHA2_256
	prefix.MhLength = -1

	dbp := helpers.DagBuilderParams{
		Dagserv:    ds.ipfs,
		Maxlinks:   helpers.DefaultLinksPerBlock,
		CidBuilder: &prefix,
	}

	var chunkerName string
	switch cfg.Chunker {
	case pb.Chunker_BOXO: //pb.Chunker_CHUNKER_UNSPECIFIED
		chunkerName = "" //use default chunker
	default:
		return cid.Undef, fmt.Errorf("unknown chunker")
	}

	chnk, err := chunker.FromString(r, chunkerName)
	if err != nil {
		return cid.Undef, err
	}

	if cfg.Encrypted {
		pk, err := ds.pkGetter(ctx, ds.pubID)
		if err != nil {
			return cid.Undef, err
		}

		chnk, err = newEncryptedChunker(chnk, pk)
		if err != nil {
			return cid.Undef, err
		}
	}

	dbh, err := dbp.New(chnk)
	if err != nil {
		return cid.Undef, err
	}

	n, err := balanced.Layout(dbh)
	if err != nil {
		return cid.Undef, err
	}

	c := n.Cid()

	err = ds.AddCid(ctx, c, opts...)
	if err != nil {
		return cid.Undef, err
	}

	return c, nil
}

func (ds *distributedStorage) AddCid(ctx context.Context, c cid.Cid, opts ...otter.AddOption) error {
	cfg := ds.defaultConfig()
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return err
		}
	}

	peers := storage.OrderPeersBy(ds.metrics.last, &storage.AvailableSpace{})
	if len(peers) < int(cfg.MinReplicas) {
		return fmt.Errorf("not enough peers to meet minReplica count, have %d peers, want %d", len(peers), cfg.MinReplicas)
	}

	peers = peers[:min(uint32(len(peers)), cfg.MaxReplicas)]

	bs, err := getDagBlockSize(ctx, c, ds.ipfs)
	if err != nil {
		return err
	}

	info := &pb.PinInfo{
		MinReplicas: cfg.MinReplicas,
		MaxReplicas: cfg.MaxReplicas,
		AddedTs:     uint64(time.Now().UnixMilli()),
		PinPeers:    []string{},
		TotalSize:   uint64(bs),
	}

	var errs error

	for _, peer := range peers {
		info.PinPeers = append(info.PinPeers, peer.String())
		err := ds.assignCidToPeer(ctx, c, peer)
		if err != nil {
			errs = errors.Join(errs, err)
		}
	}

	b, err := protobuf.Marshal(info)
	if err != nil {
		return err
	}

	pinSetInfoKey := datastore.KeyWithNamespaces([]string{cidPinPrefix, c.String()})
	return ds.pinManagement.Put(ctx, pinSetInfoKey, b)
}

func getDagBlockSize(ctx context.Context, c cid.Cid, ng ipld.NodeGetter) (uint, error) {
	var size uint

	n, err := ng.Get(ctx, c)
	if err != nil {
		return 0, err
	}

	size += uint(len(n.RawData()))

	walker := ipld.NewWalker(ctx, ipld.NewNavigableIPLDNode(n, ng))
	walker.Iterate(func(node ipld.NavigableNode) error {
		size += uint(len(node.GetIPLDNode().RawData()))

		return nil
	})

	return size, nil
}

func (ds *distributedStorage) assignCidToPeer(ctx context.Context, c cid.Cid, p peer.ID) error {
	info := &pb.PinState{
		Pinned:   false,
		Indirect: false,
		Removing: false,
	}

	b, err := protobuf.Marshal(info)
	if err != nil {
		return err
	}

	k := datastore.KeyWithNamespaces([]string{nodePinPrefix, p.String(), c.String()})

	return ds.pinManagement.Put(ctx, k, b)
}

func newEncryptedChunker(spl chunker.Splitter, pk id.PrivateKey) (*encryptedChunker, error) {
	pubk, err := pk.PublicKey()
	if err != nil {
		return nil, err
	}

	ad := []byte(pubk)

	sk, err := privateKeytoStorageKey(pk, nil)
	if err != nil {
		return nil, fmt.Errorf("getting storage key: %w", err)
	}

	aead, err := privateStorageAEAD(sk)
	if err != nil {
		return nil, fmt.Errorf("creating AEAD: %w", err)
	}

	return &encryptedChunker{spl, privateStorageSeal(aead, ad)}, nil
}

type encryptedChunker struct {
	splitter chunker.Splitter
	cipher   cryptoSealUnsealer
}

func (es *encryptedChunker) Reader() io.Reader {
	return nil
}

func (es *encryptedChunker) NextBytes() ([]byte, error) {
	b, err := es.splitter.NextBytes()
	if err != nil {
		return nil, err
	}

	return es.cipher(context.Background(), b)
}
