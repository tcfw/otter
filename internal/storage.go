package internal

import (
	"context"
	"fmt"
	"os"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	"github.com/tcfw/otter/pkg/config"
	"go.uber.org/zap"

	dsBadger3 "github.com/ipfs/go-ds-badger3"
)

func NewDatastoreStorage(o *Otter) (datastore.Datastore, error) {
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

	ds, err := dsBadger3.NewDatastore(dataDir, &options)
	if err != nil {
		return nil, fmt.Errorf("initing datastore: %w", err)
	}

	return ds, nil
}

type NamespacedStorage struct {
	ds     datastore.Datastore
	ns     string
	logger *zap.Logger

	seal   func(context.Context, []byte) ([]byte, error)
	unseal func(context.Context, []byte) ([]byte, error)
}

func (nss *NamespacedStorage) formatKey(k string) datastore.Key {
	return datastore.KeyWithNamespaces([]string{nss.ns, k})
}

func (nss *NamespacedStorage) Get(ctx context.Context, k string) ([]byte, error) {
	val, err := nss.ds.Get(ctx, nss.formatKey(k))
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

func (nss *NamespacedStorage) Put(ctx context.Context, k string, val []byte) error {
	if nss.seal != nil {
		sealedVal, err := nss.seal(ctx, val)
		if err != nil {
			return fmt.Errorf("sealing value: %w", err)
		}
		val = sealedVal
	}

	return nss.ds.Put(ctx, nss.formatKey(k), val)
}

func (nss *NamespacedStorage) Search(ctx context.Context, prefix string) (<-chan []byte, error) {
	q, err := nss.ds.Query(ctx, query.Query{Prefix: nss.formatKey(prefix).String()})
	if err != nil {
		return nil, err
	}

	ch := make(chan []byte)

	go func() {
		defer q.Close()
		defer close(ch)
		for {
			res, ok := q.NextSync()
			if !ok {
				return
			}

			if nss.unseal != nil {
				unsealedVal, err := nss.unseal(ctx, res.Value)
				if err != nil {
					nss.logger.Error("unsealing value during query", zap.Error(err))
					return
				}
				ch <- unsealedVal
			} else {
				ch <- res.Value
			}
		}
	}()

	return ch, nil
}

func (nss *NamespacedStorage) Delete(ctx context.Context, k string) error {
	return nss.ds.Delete(ctx, nss.formatKey(k))
}
