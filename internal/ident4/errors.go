package ident4

import (
	"errors"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/tcfw/otter/internal/ident4/pb"
	"go.uber.org/zap"
)

var (
	errorCode = map[error]pb.ErrorCode{
		errorInvalidRandomLength: pb.ErrorCode_BAD_RANDOM,
		errorUnknownProto:        pb.ErrorCode_UNKNOWN_DEST_PROTO,
		errorUnknownDestIdent:    pb.ErrorCode_UNKNOWN_DEST_PUBLIC_ID,
		errorInvalidSignature:    pb.ErrorCode_BAD_SIGNATURE,
	}
)

var (
	errorInvalidRandomLength = errors.New("invalid random seed length")
	errorUnknownProto        = errors.New("unknown destination protocol")
	errorUnknownDestIdent    = errors.New("unknown destination identity")
	errorInvalidSignature    = errors.New("invalid signature")
)

func (s *state) writeError(stream network.Stream, code pb.ErrorCode, err error) {
	msg := err.Error()

	if ccode, ok := errorCode[err]; ok {
		code = ccode
	}

	s.i4.l.Debug("sending err code to stream", zap.Any("code", code), zap.Any("msg", msg))

	if err := s.writeMsg(stream, &pb.Message{Packet: &pb.Message_Error{Error: &pb.Error{Code: code, Msg: msg}}}); err != nil {
		s.i4.l.Debug("failed to write err code", zap.Error(err))
	}
}
