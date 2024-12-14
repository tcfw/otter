package petnames

import (
	"context"
	"time"

	"github.com/tcfw/otter/pkg/id"
)

const (
	MaxNameSize = 255
)

type ClientImpl interface {
	ForPublicID(id.PublicID) (ScopedClient, error)
}

type ScopedClient interface {
	ProposedName() (string, error)
	SetProposedName(string) error

	SetLocalContact(*Contact) error
	GetLocalContact(id.PublicID) (string, error)
	RemoveLocalContact(id.PublicID) error

	ListLocalContacts(context.Context, id.PublicID) ([]Contact, error)
	SearchLocalContacts(context.Context, string) ([]Contact, error)

	SearchForEdgeNames(context.Context, id.PublicID) (<-chan *SharedContact, error)
}

type Contact struct {
	ID id.PublicID

	SharedName  string
	PrivateName string

	Added       time.Time
	LastUpdated time.Time
}

type SharedContact struct {
	Contact

	SharedBy  id.PublicID
	Signature []byte
}

type SetProposedNameRequest struct {
	Name string `json:"name"`
}
