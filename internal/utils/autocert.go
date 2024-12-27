package utils

import (
	"context"

	"github.com/ipfs/go-datastore"
)

type AutoCertDSCache struct {
	ds datastore.Datastore
	ns datastore.Key
}

func NewAutoCertDSCache(ds datastore.Datastore, ns string) *AutoCertDSCache {
	return &AutoCertDSCache{
		ds: ds,
		ns: datastore.NewKey(ns),
	}
}

// Get returns a certificate data for the specified key.
// If there's no such key, Get returns ErrCacheMiss.
func (a *AutoCertDSCache) Get(ctx context.Context, key string) ([]byte, error) {
	return a.ds.Get(ctx, a.ns.Child(datastore.NewKey(key)))
}

// Put stores the data in the cache under the specified key.
// Underlying implementations may use any data storage format,
// as long as the reverse operation, Get, results in the original data.
func (a *AutoCertDSCache) Put(ctx context.Context, key string, data []byte) error {
	return a.ds.Put(ctx, a.ns.Child(datastore.NewKey(key)), data)
}

// Delete removes a certificate data from the cache under the specified key.
// If there's no such key in the cache, Delete returns nil.
func (a *AutoCertDSCache) Delete(ctx context.Context, key string) error {
	return a.ds.Delete(ctx, a.ns.Child(datastore.NewKey(key)))
}
