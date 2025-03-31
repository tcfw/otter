package ident4

import (
	"bufio"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-varint"
	"github.com/tcfw/otter/internal/ident4/pb"
	"github.com/tcfw/otter/pkg/otter"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

const (
	protoID protocol.ID = "/otter/ident4/0.0.0"

	serviceName = "ident4"

	maxMessageSize int = 1024
	streamTimeout      = 5 * time.Second

	randSize = 32
)

type Ident4 struct {
	h host.Host
	c otter.Cryptography
	l *zap.Logger
}

func Setup(h host.Host, l *zap.Logger, c otter.Cryptography) (*Ident4, error) {
	i4 := &Ident4{h, c, l}

	h.SetStreamHandler(protoID, i4.handleStream)

	defaultClient = i4

	return i4, nil
}

func (s *state) readMsg(stream network.Stream) (*pb.Message, error) {
	// varintBuf := make([]byte, varint.MaxLenUvarint63)
	// n, err := stream.Read(varintBuf)
	// if err != nil {
	// 	return nil, err
	// }

	if s.readBuff == nil {
		s.readBuff = bufio.NewReader(stream)
	}

	length64, err := varint.ReadUvarint(s.readBuff) //bytes.NewReader(varintBuf[:n]))
	if err != nil {
		return nil, err
	}

	length := int(length64)
	if length > maxMessageSize {
		return nil, io.ErrShortBuffer
	}

	buf := make([]byte, length)
	if _, err := s.readBuff.Read(buf); err != nil {
		return nil, err
	}

	msg := &pb.Message{}

	if err := proto.Unmarshal(buf, msg); err != nil {
		return nil, err
	}

	return msg, nil
}
