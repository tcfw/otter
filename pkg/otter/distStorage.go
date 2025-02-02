package otter

import (
	"context"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
)

type DistributedStorage interface {
	AddFromSlice(context.Context, []byte, ...AddOption) (cid.Cid, error)
	AddFromReader(context.Context, io.Reader, ...AddOption) (cid.Cid, error)

	Get(context.Context, cid.Cid) (io.Reader, error)
	Info(context.Context, cid.Cid) (*StorageInfo, error)

	Remove(context.Context, cid.Cid) error
}

type StorageInfo struct {
	TotalSize   uint64
	PinnedPeers []peer.ID
	Added       time.Time
	MinReplicas uint
	MaxReplicas uint
}

type Chunker string

type Allocator string

type AddConfig struct {
	MinReplicas uint
	MaxReplicas uint
	Allocator   Allocator
	Chunker     Chunker
	Shard       bool
	Local       bool
}

type AddOption func(*AddConfig) error
