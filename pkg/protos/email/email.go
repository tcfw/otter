package email

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/tcfw/otter/pkg/otter"
	"go.uber.org/zap"
)

const (
	protoID  protocol.ID = "/otter/email/0.0.1"
	nWorkers int         = 10
)

var (
	eh = &EmailHandler{}
)

func Register(o otter.Otter) {
	eh.o = o
	eh.l = o.Logger(string(protoID))

	o.Protocols().RegisterP2PHandler(protoID, eh.handle)

	eh.startNWorkers(nWorkers)
	go eh.gwListen()
	eh.l.Info("started email listener")
}

type EmailHandler struct {
	o otter.Otter
	l *zap.Logger

	workQueue workQueue
}

func (e *EmailHandler) handle(s network.Stream) {
}
