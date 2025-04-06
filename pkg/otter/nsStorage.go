package otter

import (
	"context"
	"fmt"
	"strings"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	"go.uber.org/zap"
)

type PutHook func(datastore.Key, []byte)
type DeleteHook func(datastore.Key)

type NamespaceStorageOption func(*NamespacedStorage) error

func WithSealer(s func(ctx context.Context, b []byte) ([]byte, error)) NamespaceStorageOption {
	return func(ns *NamespacedStorage) error {
		ns.seal = s
		return nil
	}
}

func WithUnsealer(s func(ctx context.Context, b []byte) ([]byte, error)) NamespaceStorageOption {
	return func(ns *NamespacedStorage) error {
		ns.unseal = s
		return nil
	}
}

func WithPutHook(h func(PutHook)) NamespaceStorageOption {
	return func(ns *NamespacedStorage) error {
		ns.putHook = h
		return nil
	}
}

func WithDeleteHook(h func(DeleteHook)) NamespaceStorageOption {
	return func(ns *NamespacedStorage) error {
		ns.deleteHook = h
		return nil
	}
}

func NewNamespacedStorage(ds datastore.Batching, ns datastore.Key, l *zap.Logger, opts ...NamespaceStorageOption) (*NamespacedStorage, error) {
	nss := &NamespacedStorage{Datastore: ds, ns: ns, logger: l}

	for _, opt := range opts {
		if err := opt(nss); err != nil {
			return nil, err
		}
	}

	return nss, nil
}

// NamespacedStorage provides namespaced and/or encryption functions for a datastore
type NamespacedStorage struct {
	Datastore datastore.Batching

	ns     datastore.Key
	logger *zap.Logger

	seal   func(ctx context.Context, b []byte) ([]byte, error)
	unseal func(ctx context.Context, b []byte) ([]byte, error)

	putHook    func(PutHook)
	deleteHook func(DeleteHook)
}

func trimNamespacePrefix(prefix datastore.Key, k datastore.Key) datastore.Key {
	return datastore.NewKey(strings.TrimPrefix(k.String(), prefix.String()))
}

func (nss *NamespacedStorage) Sync(ctx context.Context, k datastore.Key) error {
	return nss.Datastore.Sync(ctx, nss.formatKey(k))
}

func (nss *NamespacedStorage) PutHook(f PutHook) {
	nss.putHook(func(k datastore.Key, b []byte) {
		if k.IsDescendantOf(nss.ns) {
			f(trimNamespacePrefix(nss.ns, k), b)
		}
	})
}

func (nss *NamespacedStorage) DeleteHook(f DeleteHook) {
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

func (nss *NamespacedStorage) GetSize(ctx context.Context, k datastore.Key) (int, error) {
	return nss.Datastore.GetSize(ctx, nss.formatKey(k))
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

func (nss *NamespacedStorage) Batch(ctx context.Context) (datastore.Batch, error) {
	b, err := nss.Datastore.Batch(ctx)
	if err != nil {
		return nil, err
	}

	return &NamespacedBatch{batch: b, formatKey: nss.formatKey}, nil
}

type NamespacedBatch struct {
	batch     datastore.Batch
	formatKey func(datastore.Key) datastore.Key
}

func (nsb *NamespacedBatch) Put(ctx context.Context, key datastore.Key, value []byte) error {
	return nsb.batch.Put(ctx, nsb.formatKey(key), value)
}

func (nsb *NamespacedBatch) Delete(ctx context.Context, key datastore.Key) error {
	return nsb.batch.Delete(ctx, nsb.formatKey(key))
}

func (nsb *NamespacedBatch) Commit(ctx context.Context) error {
	return nsb.batch.Commit(ctx)
}

var _ query.Results = (*NamespacedQueryResults)(nil)

type NamespacedQueryResults struct {
	ctx context.Context
	nss *NamespacedStorage
	q   query.Query
	r   query.Results
}

func (nsr *NamespacedQueryResults) Query() query.Query {
	return nsr.q
}

func (nsr *NamespacedQueryResults) Done() <-chan struct{} {
	return nsr.r.Done()
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
