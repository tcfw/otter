package ident4

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rand"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-varint"
	"github.com/tcfw/otter/internal/ident4/pb"
	"github.com/tcfw/otter/pkg/id"
	"google.golang.org/protobuf/proto"
	protobuf "google.golang.org/protobuf/proto"
)

type state struct {
	i4 *Ident4

	hello    *pb.Hello
	helloSig *pb.HelloSig

	okSig *pb.Sig

	nextProto protocol.ID
	destIdent id.PublicID

	writeBuff []byte
	readBuff  *bufio.Reader
}

func (s *state) validateHello(ctx context.Context) error {
	if len(s.hello.Random) != randSize {
		return errorInvalidRandomLength
	}

	if _, err := id.PublicID(s.hello.SrcPublicID).AsLibP2P(); err != nil {
		return err
	}

	for _, proto := range s.i4.h.Mux().Protocols() {
		if proto == protocol.ID(s.hello.NextProto) {
			s.nextProto = proto
		}
	}

	if s.nextProto == "" {
		return errorUnknownProto
	}

	keys, err := s.i4.c.KeyStore().Keys(ctx)
	if err != nil {
		return err
	}

	for _, k := range keys {
		if k == id.PublicID(s.hello.DestPublicID) {
			s.destIdent = k
		}
	}

	if s.destIdent == "" {
		return errorUnknownDestIdent
	}

	return nil
}

func (s *state) sendHelloSig(ctx context.Context, stream network.Stream) error {
	s.helloSig = &pb.HelloSig{Random: make([]byte, randSize)}

	s.helloSig.Random = make([]byte, randSize)
	if _, err := rand.Read(s.helloSig.Random); err != nil {
		return err
	}

	sig, err := s.createSignature(ctx, s.destIdent)
	if err != nil {
		return err
	}

	s.helloSig.Sig = &pb.Sig{Signature: sig}

	return s.writeMsg(stream, &pb.Message{Packet: &pb.Message_HelloSig{HelloSig: s.helloSig}})
}

func (s *state) sendOkSig(ctx context.Context, stream network.Stream) error {
	sig, err := s.createSignature(ctx, id.PublicID(s.hello.SrcPublicID))
	if err != nil {
		return err
	}

	s.helloSig.Sig = &pb.Sig{Signature: sig}

	return s.writeMsg(stream, &pb.Message{Packet: &pb.Message_OkSig{OkSig: &pb.Sig{Signature: sig}}})
}

func (s *state) createSignature(ctx context.Context, pub id.PublicID) ([]byte, error) {
	sd, err := s.sigData()
	if err != nil {
		return nil, err
	}

	sig, err := s.i4.c.KeyStore().Sign(ctx, pub, sd, crypto.Hash(0))
	if err != nil {
		return nil, err
	}

	return sig, nil
}

func (s *state) validateOkSig(ctx context.Context) error {
	return s.validateSig(ctx, id.PublicID(s.hello.SrcPublicID), s.okSig.Signature)
}

func (s *state) validateHelloSig(ctx context.Context) error {
	return s.validateSig(ctx, id.PublicID(s.hello.DestPublicID), s.helloSig.Sig.Signature)
}

func (s *state) validateSig(ctx context.Context, key id.PublicID, sig []byte) error {
	sd, err := s.sigData()
	if err != nil {
		return err
	}

	pk, err := key.AsLibP2P()
	if err != nil {
		return err
	}

	ok, err := pk.Verify(sd, sig)
	if err != nil {
		return err
	}
	if !ok {
		return errorInvalidSignature
	}

	return nil
}

func (s *state) sigData() ([]byte, error) {
	hb, err := protobuf.Marshal(s.hello)
	if err != nil {
		return nil, err
	}

	hs := &pb.HelloSig{Random: s.helloSig.Random}

	hsb, err := protobuf.Marshal(hs)
	if err != nil {
		return nil, err
	}

	//helloSig must include server side signature too

	hb = append(hb, hsb...)
	return hb, nil
}

func (s *state) sendOk(ctx context.Context, stream network.Stream) error {
	return s.writeMsg(stream, &pb.Message{Packet: &pb.Message_Ok{Ok: &pb.Ok{}}})
}

func (s *state) sendHello(stream network.Stream) error {
	return s.writeMsg(stream, &pb.Message{Packet: &pb.Message_Hello{Hello: s.hello}})
}

func (s *state) writeMsg(stream network.Stream, msg *pb.Message) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	if s.writeBuff == nil {
		s.writeBuff = make([]byte, varint.MaxLenUvarint63)
	}

	s.writeBuff = s.writeBuff[:varint.MaxLenUvarint63]

	length := uint64(len(b))
	n := varint.PutUvarint(s.writeBuff, length)
	// s.writeBuff = s.writeBuff[:n]
	_, err = stream.Write(s.writeBuff[:n])
	if err != nil {
		return err
	}

	// s.writeBuff = append(s.writeBuff, b...)

	_, err = stream.Write(b)
	if err != nil {
		return err
	}

	return err
}
