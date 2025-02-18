package ident4

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/tcfw/otter/internal/ident4/pb"
	"github.com/tcfw/otter/pkg/id"
)

var (
	defaultClient *Ident4
)

func Dial(peer peer.ID, proto protocol.ID, remote id.PublicID, local id.PublicID) (network.Stream, error) {
	return defaultClient.DialContext(context.Background(), peer, proto, remote, local)
}

func DialContext(ctx context.Context, peer peer.ID, proto protocol.ID, remote id.PublicID, local id.PublicID) (network.Stream, error) {
	return defaultClient.DialContext(ctx, peer, proto, remote, local)
}

func (i4 *Ident4) Dial(peer peer.ID, proto protocol.ID, remote id.PublicID, local id.PublicID) (network.Stream, error) {
	return i4.DialContext(context.Background(), peer, proto, remote, local)
}

func (i4 *Ident4) DialContext(ctx context.Context, peer peer.ID, proto protocol.ID, remote id.PublicID, local id.PublicID) (network.Stream, error) {
	stream, err := i4.h.NewStream(ctx, peer, protoID)
	if err != nil {
		return nil, err
	}

	upgradedStream, err := i4.handshakeClient(ctx, stream, proto, remote, local)
	if err != nil {
		return nil, err
	}

	return upgradedStream, nil
}

func (i4 *Ident4) handshakeClient(ctx context.Context, s network.Stream, nextProto protocol.ID, remote id.PublicID, local id.PublicID) (network.Stream, error) {
	state := &state{
		i4: i4,
		hello: &pb.Hello{
			Random:       make([]byte, randSize),
			NextProto:    string(nextProto),
			DestPublicID: string(remote),
			SrcPublicID:  string(local),
		},
	}

	if _, err := rand.Read(state.hello.Random); err != nil {
		return nil, err
	}

	if err := state.sendHello(s); err != nil {
		return nil, err
	}

	msg, err := state.readMsg(s)
	if err != nil {
		s.Close()
		return nil, err
	}

	if err, ok := msg.GetPacket().(*pb.Message_Error); ok {
		s.Close()
		return nil, errors.New(err.Error.Msg)
	}

	if helloSig, ok := msg.GetPacket().(*pb.Message_HelloSig); ok {
		state.helloSig = helloSig.HelloSig
	} else {
		s.Close()
		return nil, errors.New("unexpected msg type")
	}

	if err := state.validateHelloSig(ctx); err != nil {
		state.writeError(s, pb.ErrorCode_UNKNOWN, err)
		s.Close()
		return nil, err
	}

	if err := state.sendOkSig(ctx, s); err != nil {
		s.Close()
		return nil, err
	}

	msg2, err := state.readMsg(s)
	if err != nil {
		s.Close()
		return nil, err
	}

	if err, ok := msg2.GetPacket().(*pb.Message_Error); ok {
		s.Close()
		return nil, errors.New(err.Error.Msg)
	}

	//ok is empty, so just validate type and move on
	if _, ok := msg2.GetPacket().(*pb.Message_Ok); !ok {
		s.Close()
		return nil, errors.New("unexpected msg type")
	}

	upgradedStream := &i4Stream{
		buf:      state.readBuff,
		Stream:   s,
		localID:  id.PublicID(state.hello.SrcPublicID),
		remoteID: id.PublicID(state.hello.DestPublicID),
		proto:    state.nextProto,
	}

	return upgradedStream, nil
}
