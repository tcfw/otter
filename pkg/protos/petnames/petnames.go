package petnames

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/tcfw/otter/pkg/otter"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.uber.org/zap"
)

var (
	pnh = &PetnamesHandler{}
)

const (
	protoID protocol.ID = "/otter/petnames/0.0.1"
)

func Register(o otter.Otter) {
	pnh.o = o
	pnh.l = o.Logger("petnames")

	o.Protocols().RegisterP2PHandler(protoID, pnh.handle)

	o.Protocols().RegisterAPIHandlers(func(r *mux.Router) {
		sr := r.PathPrefix("/petnames/").Subrouter()
		sr.HandleFunc("/setproposedname", pnh.apiHandle_SetProposedName).Methods(http.MethodPost)
	})
}

type PetnamesHandler struct {
	o otter.Otter
	l *zap.Logger
}

func (p *PetnamesHandler) handle(s network.Stream) {
}
