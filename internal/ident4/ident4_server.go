package ident4

import (
	"bufio"
	"context"
	"errors"
	"net"
	"time"

	"github.com/tcfw/otter/internal/ident4/pb"
	"github.com/tcfw/otter/pkg/id"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	mmux "github.com/multiformats/go-multistream"
	"go.uber.org/zap"
)

func (i4 *Ident4) handleStream(s network.Stream) {
	i4.l.Debug("new ident4 stream", zap.Any("peer", s.Conn().RemotePeer()))

	if err := s.Scope().SetService(serviceName); err != nil {
		i4.l.Debug("error attaching stream to ident4 service", zap.Error(err))
		s.Reset()
		return
	}

	if err := s.Scope().ReserveMemory(maxMessageSize, network.ReservationPriorityAlways); err != nil {
		i4.l.Debug("error reserving memory for stream", zap.Error(err))
		s.Reset()
		return
	}
	defer s.Scope().ReleaseMemory(maxMessageSize)

	s.SetReadDeadline(time.Now().Add(streamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	state := &state{i4: i4}

	msg, err := state.readMsg(s)
	if err != nil {
		state.writeError(s, pb.ErrorCode_UNKNOWN, err)
		s.Close()
		return
	}

	if h, ok := msg.GetPacket().(*pb.Message_Hello); ok {
		state.hello = h.Hello
	} else {
		state.writeError(s, pb.ErrorCode_UNKNOWN, errors.New("unexpected msg type"))
		s.Close()
		return
	}

	if err := state.validateHello(ctx); err != nil {
		state.writeError(s, pb.ErrorCode_UNKNOWN, err)
		s.Close()
		return
	}

	if err := state.sendHelloSig(ctx, s); err != nil {
		state.writeError(s, pb.ErrorCode_UNKNOWN, err)
		s.Close()
		return
	}

	msg2, err := state.readMsg(s)
	if err != nil {
		state.writeError(s, pb.ErrorCode_UNKNOWN, err)
		s.Close()
		return
	}

	if err, ok := msg2.GetPacket().(*pb.Message_Error); ok {
		i4.l.Debug("got err on okSig", zap.Any("msg", err.Error.Msg), zap.Any("code", err.Error.Code))
		s.Close()
		return
	}

	if okSig, ok := msg2.GetPacket().(*pb.Message_OkSig); ok {
		state.okSig = okSig.OkSig
	} else {
		state.writeError(s, pb.ErrorCode_UNKNOWN, errors.New("unexpected msg type"))
		s.Close()
		return
	}

	if err := state.validateOkSig(ctx); err != nil {
		state.writeError(s, pb.ErrorCode_UNKNOWN, err)
		s.Close()
		return
	}

	if err := state.sendOk(ctx, s); err != nil {
		state.writeError(s, pb.ErrorCode_UNKNOWN, err)
		s.Close()
		return
	}

	s.SetReadDeadline(time.Time{})

	i4.upgradeStream(s, state)
}

func (i4 *Ident4) upgradeStream(stream network.Stream, state *state) {
	w, r := net.Pipe()
	defer r.Close()
	defer w.Close()

	go func() {
		if _, err := mmux.SelectOneOf[protocol.ID]([]protocol.ID{state.nextProto}, w); err != nil {
			i4.l.Error("failed to negotiate proto", zap.Error(err))
		}
	}()

	proto, handle, err := i4.h.Mux().Negotiate(r)
	if err != nil {
		i4.l.Error("failed to negotiate proto in server", zap.Error(err))
		return
	}

	if proto != state.nextProto {
		stream.Close()
		return
	}

	if err := stream.SetProtocol(state.nextProto); err != nil {
		i4.l.Debug("error setting stream protocol", zap.Error(err))
		stream.Reset()
		return
	}

	s := &i4Stream{
		buf:      state.readBuff,
		Stream:   stream,
		localID:  id.PublicID(state.hello.DestPublicID),
		remoteID: id.PublicID(state.hello.SrcPublicID),
		proto:    state.nextProto,
	}

	handle(state.nextProto, s)
}

type i4Stream struct {
	network.Stream

	localID  id.PublicID
	remoteID id.PublicID
	proto    protocol.ID
	buf      *bufio.Reader
}

func (s *i4Stream) Conn() network.Conn {
	return &i4Conn{Conn: s.Stream.Conn(), stream: s}
}

func (s *i4Stream) Read(p []byte) (n int, err error) {
	return s.buf.Read(p)
}

type i4Conn struct {
	network.Conn

	stream *i4Stream
}

// LocalPeer returns our peer ID
func (c *i4Conn) LocalPeer() peer.ID {
	return peer.ID(c.stream.localID)
}

// RemotePeer returns the peer ID of the remote peer.
func (c *i4Conn) RemotePeer() peer.ID {
	return peer.ID(c.stream.remoteID)
}

// RemotePublicKey returns the public key of the remote peer.
func (c *i4Conn) RemotePublicKey() crypto.PubKey {
	id, _ := c.stream.remoteID.AsLibP2P()

	return id
}
