package internal

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	"github.com/jbenet/goprocess"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/id"
	"go.uber.org/zap"

	dsBadger3 "github.com/ipfs/go-ds-badger3"
)

const (
	systemKeyPrefix  = "/system/"
	publicKeyPrefix  = "/account/public/"
	privateKeyPrefix = "/account/private/"
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

type cryptoSealUnSeal func(ctx context.Context, b []byte) ([]byte, error)

type StorageClasses struct {
	o *Otter
}

// Public provides a storage class for a given public key
// No values or keys are encrypted and objects are assumed to *not* be globally shareable
func (sc *StorageClasses) System() (*NamespacedStorage, error) {
	return &NamespacedStorage{
		Datastore: sc.o.ds,
		logger:    sc.o.logger.Named("system_storage"),
		ns:        systemKeyPrefix,
	}, nil
}

// Public provides a storage class for a given public key
// No values or keys are encrypted and objects are assumed to be globally shareable
func (sc *StorageClasses) Public(pub id.PublicID) (*NamespacedStorage, error) {
	syncer, err := sc.o.GetOrNewAccountSyncer(sc.o.ctx, pub)
	if err != nil {
		return nil, fmt.Errorf("getting account syncer: %w", err)
	}

	return &NamespacedStorage{
		Datastore: syncer.publicSyncer,
		logger:    sc.o.logger.Named("public_storage"),
		ns:        publicKeyPrefix + string(pub),
	}, nil
}

// Private provides a storage class for a given private key
// All values will be sealed using the derrived storage key
// Keys are not encrypted
//
// Values are assumed to be shareable once sealed
func (sc *StorageClasses) Private(pk id.PrivateKey) (*NamespacedStorage, error) {
	pubk, err := pk.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("getting public key: %w", err)
	}

	syncer, err := sc.o.GetOrNewAccountSyncer(sc.o.ctx, pubk)
	if err != nil {
		return nil, fmt.Errorf("getting account syncer: %w", err)
	}

	sk, err := privateKeytoStorageKey(pk)
	if err != nil {
		return nil, fmt.Errorf("getting storage key: %w", err)
	}

	aead, err := privateStorageAEAD(sk)
	if err != nil {
		return nil, fmt.Errorf("creating AEAD: %w", err)
	}

	ad := []byte(pubk)

	ns := &NamespacedStorage{
		Datastore: syncer.privateSyncer,
		logger:    sc.o.logger.Named("private_storage"),
		ns:        privateKeyPrefix + string(pubk),
		seal:      privateStorageSeal(aead, ad),
		unseal:    privateStorageUnseal(aead, ad),
	}

	return ns, nil
}

// NamespacedStorage provides namespaced and/or encryption functions for a datastore
type NamespacedStorage struct {
	datastore.Datastore
	ns     string
	logger *zap.Logger

	seal   cryptoSealUnSeal
	unseal cryptoSealUnSeal
}

// formatKey wraps the key with the storage class namespace
func (nss *NamespacedStorage) formatKey(k string) datastore.Key {
	return datastore.KeyWithNamespaces([]string{nss.ns, k})
}

// Get retreives a value for the given value from the datastore
// unsealing the value if configured
func (nss *NamespacedStorage) Get(ctx context.Context, k string) ([]byte, error) {
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
func (nss *NamespacedStorage) Has(ctx context.Context, k string) (bool, error) {
	return nss.Datastore.Has(ctx, nss.formatKey(k))
}

// Put adds a key/value to the datastore
// sealing the value if configured
func (nss *NamespacedStorage) Put(ctx context.Context, k string, val []byte) error {
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
	q.Prefix = nss.formatKey(q.Prefix).String()

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
func (nss *NamespacedStorage) Delete(ctx context.Context, k string) error {
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
			r.Key = strings.TrimPrefix(r.Key, nsr.nss.ns)
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

	val.Key = strings.TrimPrefix(val.Key, nsr.nss.ns)

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

		r.Key = strings.TrimPrefix(r.Key, nsr.nss.ns)
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
