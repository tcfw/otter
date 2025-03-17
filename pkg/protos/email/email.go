package email

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"time"

	"github.com/ipfs/go-datastore"
	"github.com/multiformats/go-multihash"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/protos/email/pb"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio/pbio"
	"go.uber.org/zap"
	protobuf "google.golang.org/protobuf/proto"
)

const (
	protoID  protocol.ID = "/otter/email/0.0.1"
	nWorkers int         = 10
)

var (
	eh = &EmailHandler{
		workQueue: make(workQueue, 100),
	}
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
	if err := s.Scope().SetService(string(protoID)); err != nil {
		e.l.Error("failed to set scope service identifier", zap.Error(err))
		return
	}

	if err := s.Scope().ReserveMemory(maxMessageBytes, network.ReservationPriorityMedium); err != nil {
		e.l.Error("failed to reserve memroy for handling email body", zap.Error(err))
		return
	}
	defer s.Scope().ReleaseMemory(maxMessageBytes)

	r := pbio.NewDelimitedReader(s, maxMessageBytes)
	w := pbio.NewDelimitedWriter(s)

	req := &pb.Request{}

	if err := r.ReadMsg(req); err != nil {
		e.l.Error("reading req", zap.Error(err))
		return
	}

	resp := &pb.Response{}

	err := e.handleRequest(s.Conn().RemotePeer(), req)
	if err != nil {
		resp.Error = err.Error()
	}

	if err := w.WriteMsg(resp); err != nil {
		e.l.Error("writing response", zap.Error(err))
		return
	}
}

func (e *EmailHandler) handleRequest(p peer.ID, req *pb.Request) error {
	if req.PublicID != p.String() {
		return errors.New("public ID mismatch")
	}

	if err := e.validateSignature(p, req); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	switch req.Data.(type) {
	case *pb.Request_ReceiveEmail:
		err := e.handleReceiveEmail(p, req.GetReceiveEmail())
		if err != nil {
			return err
		}
	default:
		return errors.New("unknown request type")
	}

	return nil
}

func (e *EmailHandler) validateSignature(p peer.ID, req *pb.Request) error {
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

	pk, err := p.ExtractPublicKey()
	if err != nil {
		return err
	}

	ok, err := pk.Verify(d, req.Signature)
	if err != nil {
		return err
	}

	if !ok {
		return errors.New("invalid signature")
	}

	return nil
}

func (e *EmailHandler) handleReceiveEmail(p peer.ID, req *pb.ReceiveEmail) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	addr, err := mail.ParseAddress(req.To)
	if err != nil {
		return err
	}

	publicID, _, err := SplitAddrUserDomain(addr.Address)
	if err != nil {
		return err
	}

	pid := id.PublicID(publicID)

	spamPrependStr, isSpam, err := e.checkSpam(p, req)
	if err != nil {
		return err
	}

	req.Envelope = []byte(fmt.Sprintf("Authentication-Results: %s\r\n%s", spamPrependStr, req.Envelope))

	if isSpam {
		e.l.Warn("SPAM MSG", zap.Any("spam result", spamPrependStr))
	}

	e.l.Info("got email", zap.String("msg", string(req.Envelope)))

	msgId, err := messageID(req.Envelope)
	if err != nil {
		return err
	}

	privStore, err := e.o.Storage().PrivateFromPublic(pid)
	if err != nil {
		return err
	}

	ds, err := otter.NewNamespacedStorage(privStore, datastore.NewKey("/emails/"), e.l)
	if err != nil {
		return err
	}
	k := datastore.KeyWithNamespaces([]string{"email", msgId})

	exists, err := ds.Has(ctx, k)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("email already received")
	}

	distStore, err := e.o.DistributedStorage(pid)
	if err != nil {
		return err
	}

	c, err := distStore.AddFromSlice(context.Background(), req.Envelope, otter.WithEncrypted(), otter.WithMinReplicas(1))
	if err != nil {
		return err
	}

	return ds.Put(ctx, k, c.Bytes())
}

func messageID(envl []byte) (string, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(envl))
	if err != nil {
		return "", err
	}

	d := envl

	envlMsgID := msg.Header.Get("message-id")
	if envlMsgID != "" {
		d = []byte(envlMsgID)
	}

	mh, err := multihash.Sum(d, multihash.SHA2_256, -1)
	if err != nil {
		return "", err
	}

	return mh.B58String(), nil
}
