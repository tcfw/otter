package petnames

import (
	"context"

	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/protos/petnames/pb"
)

const (
	MaxNameSize = 255
)

type ClientImpl interface {
	ForPublicID(id.PublicID) (ScopedClient, error)
}

type ScopedClient interface {
	ProposedName() (string, error)
	SetProposedName(context.Context, string) error

	SetLocalContact(context.Context, *pb.Contact) error
	GetLocalContact(context.Context, id.PublicID) (*pb.Contact, error)
	RemoveLocalContact(context.Context, id.PublicID) error

	CountLocalContacts(context.Context) (int, error)
	SearchLocalContacts(context.Context, string) ([]*pb.Contact, error)
	ListLocalContacts(context.Context, int, int) ([]*pb.Contact, error)
	ListAllLocalContactsBy(context.Context, func(a, b *pb.Contact) int) (<-chan *pb.Contact, error)

	SearchForEdgeNames(context.Context, id.PublicID) (<-chan *pb.DOSName, error)
}
