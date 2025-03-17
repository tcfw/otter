package otter

import (
	"context"
	"errors"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/tcfw/otter/pkg/otter/pb"
)

type DistributedStorage interface {
	AddFromSlice(context.Context, []byte, ...AddOption) (cid.Cid, error)
	AddFromReader(context.Context, io.Reader, ...AddOption) (cid.Cid, error)
	AddCid(context.Context, cid.Cid, ...AddOption) error

	Get(context.Context, cid.Cid) (io.ReadSeekCloser, error)
	GetEncrypted(context.Context, cid.Cid) (io.ReadCloser, error)
	Info(context.Context, cid.Cid) (*pb.PinInfo, error)

	Remove(context.Context, cid.Cid) error
}

type AddOption func(*pb.AddConfig) error

var (
	ErrNotFound = errors.New("not found")
)

func WithEncrypted() AddOption {
	return func(ac *pb.AddConfig) error {
		ac.Encrypted = true
		return nil
	}
}

func WithMinReplicas(n int) AddOption {
	return func(ac *pb.AddConfig) error {
		ac.MinReplicas = uint32(n)
		return nil
	}
}

func WithMaxReplicas(n int) AddOption {
	return func(ac *pb.AddConfig) error {
		ac.MaxReplicas = uint32(n)
		return nil
	}
}
