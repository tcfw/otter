package petnames

import (
	"context"
	"math/rand"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/protos/petnames/pb"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio/pbio"
	"go.uber.org/zap"
)

var (
	pnh = &PetnamesHandler{}
)

const (
	protoID protocol.ID = "/otter/petnames/0.0.1"

	reqMaxSize    = 2048
	streamTimeout = 1 * time.Second

	maxRequestedIDs       = 10
	maxRequestProbability = 0.5
)

func Register(o otter.Otter) {
	pnh.o = o
	pnh.l = o.Logger("petnames")

	o.Protocols().RegisterP2PHandler(protoID, pnh.handle)

	o.Protocols().RegisterAPIHandlers(func(r *mux.Router) {
		sr := r.PathPrefix("/petnames/").Subrouter()

		sr.HandleFunc("/search", pnh.apiHandle_SearchDHT).Methods(http.MethodGet)

		sr.HandleFunc("/proposedname", pnh.apiHandle_GetProposedName).Methods(http.MethodGet)
		sr.HandleFunc("/setproposedname", pnh.apiHandle_SetProposedName).Methods(http.MethodPost)

		sr.HandleFunc("/set", pnh.apiHandle_SetContact).Methods(http.MethodPost)
		sr.HandleFunc("/list", pnh.apiHandle_ListContact).Methods(http.MethodGet)
	})

	go pnh.broadcast()
}

type PetnamesHandler struct {
	o otter.Otter
	l *zap.Logger
}

func (p *PetnamesHandler) handle(s network.Stream) {
	if err := s.Scope().SetService(string(protoID)); err != nil {
		p.l.Debug("error attaching stream to service", zap.Error(err))
		s.Reset()
		return
	}

	if err := s.Scope().ReserveMemory(reqMaxSize, network.ReservationPriorityLow); err != nil {
		p.l.Debug("error reserving memory for stream", zap.Error(err))
		s.Reset()
		return
	}
	defer s.Scope().ReleaseMemory(reqMaxSize)
	s.SetReadDeadline(time.Now().Add(streamTimeout))

	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	r := pbio.NewDelimitedReader(s, reqMaxSize)
	defer r.Close()

	req := &pb.LookupRequest{}
	err := r.ReadMsg(req)
	if err != nil {
		p.l.Debug("reading req", zap.Error(err))
		return
	}

	resp := &pb.LookupResponse{}
	var respMu sync.Mutex

	if req.SuggestedNodeProbability > maxRequestProbability {
		if err := sendError(s, pb.ErrorCode_OVER_MAX_PROBABILITY); err != nil {
			p.l.Debug("sending error response", zap.Error(err))
		}
		return
	}

	if len(req.Id) > maxRequestedIDs {
		if err := sendError(s, pb.ErrorCode_OVER_MAX_IDS); err != nil {
			p.l.Debug("sending error response", zap.Error(err))
		}
		return
	}

	pid := id.PublicID(string(s.Conn().LocalPeer()))
	book, err := p.ForPublicID(pid)
	if err != nil {
		p.l.Error("opening contacts for public ID", zap.Error(err))
		sendError(s, pb.ErrorCode_INTERNAL)
		return
	}

	idCh := make(chan string)
	go func() {
		defer close(idCh)
		for _, id := range req.Id {
			idCh <- id
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	for range 2 {
		go func() {
			defer wg.Done()

			for cid := range idCh {
				c, err := book.GetLocalContact(ctx, id.PublicID(cid))
				if err != nil {
					p.l.Error("looking up contract", zap.Error(err))
					continue
				}

				if c.SharedName != nil {
					respMu.Lock()
					resp.SharedNames = append(resp.SharedNames, &pb.SharedContact{
						Id:         cid,
						SharedName: c.SharedName,
					})
					respMu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	count, err := book.CountLocalContacts(ctx)
	if err == nil {
		resp.ContactSize = uint32(count)
	}

	if len(resp.SharedNames) != len(req.Id) {
		resp.TryContactId, err = probabalisticListContactsToTry(ctx, book, req.SuggestedNodeProbability, 20)
		if err != nil {
			p.l.Error("getting list of contacts to try", zap.Error(err))
			sendError(s, pb.ErrorCode_INTERNAL)
			return
		}
	}

	err = streamResponse(s, resp)
	if err != nil {
		p.l.Error("sending response", zap.Error(err))
	}
}

func probabalisticListContactsToTry(ctx context.Context, book ScopedClient, prob float64, max int) ([]string, error) {
	bl, err := book.ListLocalContacts(ctx)
	if err != nil {
		return nil, err
	}

	slices.SortFunc(bl, sortByNumContacts)

	contacts := []string{}

	for _, c := range slices.Backward(bl) {
		if rand.Float64() >= prob {
			continue
		}

		contacts = append(contacts, c.Id)

		if len(contacts) >= max {
			break
		}
	}

	//probability wasn't on the requesters side here and we so far have suggested no
	//new contacts to try, just add at least 1
	if len(contacts) == 0 && len(bl) != 0 {
		contacts = append(contacts, bl[0].Id)
	}

	return contacts, nil
}

func sortByNumContacts(a *pb.Contact, b *pb.Contact) int {
	switch true {
	case a.NumberOfContacts > b.NumberOfContacts:
		return 1
	case a.NumberOfContacts < b.NumberOfContacts:
		return -1
	default:
		return 0
	}
}

func sendError(s network.Stream, errCode pb.ErrorCode) error {
	resp := &pb.LookupResponse{Error: errCode}

	return streamResponse(s, resp)
}

func streamResponse(s network.Stream, resp *pb.LookupResponse) error {
	w := pbio.NewDelimitedWriter(s)
	defer w.Close()

	return w.WriteMsg(resp)
}
