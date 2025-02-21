package email

import (
	"context"
	"errors"
	"net"
	"net/mail"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/tcfw/otter/internal/ident4"
	"github.com/tcfw/otter/internal/utils"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/protos/email/pb"
	"go.uber.org/zap"
	protobuf "google.golang.org/protobuf/proto"
)

const (
	workerHandleTimeout = 10 * time.Second
	maxTries            = 3
)

type queueJob struct {
	Hello    string
	RemoteIP net.TCPAddr
	From     string
	To       string
	Envl     []byte
	Tries    int
	Release  func()
}

type workQueue chan *queueJob

func (eh *EmailHandler) startNWorkers(n int) {
	for i := 0; i < n; i++ {
		go eh.worker(i)
	}
}

func (eh *EmailHandler) worker(id int) {
	for job := range eh.workQueue {
		if job.Tries > maxTries {
			eh.handleJobExpired(job)
		}

		err := eh.handleJob(job)
		if err != nil {
			eh.l.Error("handling email", zap.Any("worker", id), zap.Error(err))

			go func() {
				time.Sleep(1 * time.Second)

				job.Tries++
				eh.workQueue <- job
			}()
		} else {
			job.Release()
		}
	}
}

func (eh *EmailHandler) handleJobExpired(job *queueJob) {
	defer job.Release()
}

func (eh *EmailHandler) handleJob(job *queueJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), workerHandleTimeout)
	defer cancel()

	addr, err := mail.ParseAddress(job.To)
	if err != nil {
		return err
	}

	publicID, _, err := SplitAddrUserDomain(addr.Address)
	if err != nil {
		return err
	}

	pid := id.PublicID(publicID)

	peers, err := eh.o.ResolveOtterNodesForKey(ctx, pid)
	if err != nil {
		return err
	}

	p, err := utils.FirstOnlinePeer(ctx, peers, eh.o.Protocols().P2P())
	if err != nil {
		return err
	}

	return eh.sendEnvlToPeer(ctx, job, p, pid)
}

func (eh *EmailHandler) signRequest(req *pb.Request) error {
	var msg protobuf.Message

	switch t := req.Data.(type) {
	case *pb.Request_ReceiveEmail:
		msg = t.ReceiveEmail
	default:
		return errors.New("unknown data type")
	}

	d, err := protobuf.Marshal(msg)
	if err != nil {
		return err
	}

	privk, err := eh.o.Crypto().HostKey()
	if err != nil {
		return err
	}

	sig, err := privk.Sign(d)
	if err != nil {
		return err
	}
	req.Signature = sig
	req.PublicID = eh.o.HostID().String()

	return nil
}

func (eh *EmailHandler) sendEnvlToPeer(ctx context.Context, job *queueJob, p peer.ID, pid id.PublicID) error {
	req := &pb.Request{
		PublicID: eh.o.HostID().String(),
		Data: &pb.Request_ReceiveEmail{
			ReceiveEmail: &pb.ReceiveEmail{
				Hello:    job.Hello,
				Ip:       job.RemoteIP.String(),
				From:     job.From,
				To:       job.To,
				Envelope: job.Envl,
			},
		},
	}

	if err := eh.signRequest(req); err != nil {
		return err
	}

	if p.String() == eh.o.HostID().String() {
		//loopback to self
		return eh.handleRequest(p, req)
	}

	//eh.o.Protocols().P2P().NewStream(ctx, p, protoID)
	stream, err := ident4.DialContext(ctx, p, protoID, pid, id.PublicID(eh.o.HostID().String()))
	if err != nil {
		return err
	}
	defer stream.Close()

	r := pbio.NewDelimitedReader(stream, maxMessageBytes)
	w := pbio.NewDelimitedWriter(stream)
	if err := w.WriteMsg(req); err != nil {
		eh.l.Error("failed to write request to remote node", zap.Error(err))
		return err
	}

	resp := &pb.Response{}

	if err := r.ReadMsg(resp); err != nil {
		eh.l.Error("failed to read response from remote node", zap.Error(err))
		return err
	}

	return nil
}
