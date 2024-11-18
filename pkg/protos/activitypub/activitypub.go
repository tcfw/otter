package activitypub

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/tcfw/otter/pkg/otter"
)

const (
	protoID protocol.ID = "/otter/activitypub/0.0.1"
)

var (
	aph = &ActivityPubHandler{}
)

func Register(o otter.Otter) {
	aph.o = o
	o.Protocols().RegisterP2PHandler(protoID, aph.handle)
}

type ActivityPubHandler struct {
	o otter.Otter
}

func (a *ActivityPubHandler) handle(s network.Stream) {
	defer s.Close()
}
