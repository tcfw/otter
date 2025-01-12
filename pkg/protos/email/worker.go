package email

import (
	"context"
	"net"
	"net/mail"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/tcfw/otter/internal/utils"
	"github.com/tcfw/otter/pkg/id"
	"go.uber.org/zap"
)

const (
	workerHandleTimeout = 10 * time.Second
)

type queueJob struct {
	EnvlRemote net.TCPAddr
	EnvlFrom   string
	EnvlTo     string
	Envl       []byte
	Tries      int
}

type workQueue chan *queueJob

func (eh *EmailHandler) startNWorkers(n int) {
	for i := 0; i < n; i++ {
		go eh.worker(i)
	}
}

func (eh *EmailHandler) worker(id int) {
	for job := range eh.workQueue {
		err := eh.handleJob(job)
		if err != nil {
			eh.l.Error("handling email", zap.Any("worker", id), zap.Error(err))
		}
	}
}

func (eh *EmailHandler) handleJob(job *queueJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), workerHandleTimeout)
	defer cancel()

	addr, err := mail.ParseAddress(job.EnvlTo)
	if err != nil {
		return err
	}

	publicID, _, err := SplitAddrUserDomain(addr.Address)
	if err != nil {
		return err
	}

	peers, err := eh.o.ResolveOtterNodesForKey(ctx, id.PublicID(publicID))
	if err != nil {
		return err
	}

	p, err := utils.FirstOnlinePeer(ctx, peers, eh.o.Protocols().P2P())
	if err != nil {
		return err
	}

	return eh.sendEnvlToPeer(ctx, job, p)
}

func (eh *EmailHandler) sendEnvlToPeer(ctx context.Context, job *queueJob, p peer.ID) error {
	stream, err := eh.o.Protocols().P2P().NewStream(ctx, p, protoID)
	if err != nil {
		return err
	}
	defer stream.Close()

	return nil
}
