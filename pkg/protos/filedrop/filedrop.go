package filedrop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/tcfw/otter/pkg/otter"
	"go.uber.org/zap"
)

/*
FileDrop is an authenticated request-to-download/share mechanism.
The receiver generates a token and sends the token to the sender by any means.
The sender, using the token to connect to the stream can then send the file directly
over the stream, or just the CID to fetch.

If the transfer is direct, the receiver ack's the request before the sender streams the
file content.
*/

const (
	protoID protocol.ID = "/otter/filedrop/0.0.1"

	reqMaxSize = 2048
)

var (
	fdh = &FileDropHandler{}
)

func Register(o otter.Otter) {
	fdh.o = o
	fdh.l = o.Logger(string(protoID))

	o.Protocols().RegisterP2PHandler(protoID, fdh.handle)
}

type FileDropHandler struct {
	o otter.Otter
	l *zap.Logger
}

func (a *FileDropHandler) handle(s network.Stream) {
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reqBytes := make([]byte, reqMaxSize)
	n, err := s.Read(reqBytes)
	if err != nil {
		a.l.Error("reading request", zap.Error(err))
		return
	}

	req := &TransferRequest{}

	if err := json.Unmarshal(reqBytes[:n], req); err != nil {
		a.l.Error("unmarshalling request", zap.Error(err))
		return
	}

	st := &ShareToken{}

	rst, err := base64.RawStdEncoding.DecodeString(req.ShareToken)
	if err != nil {
		a.l.Error("decoding share key", zap.Error(err))
		return
	}

	if err := json.Unmarshal(rst, st); err != nil {
		a.l.Error("unmarshalling share key", zap.Error(err))
		return
	}

	pk, err := a.o.Crypto().HostKey()
	if err != nil {
		a.l.Error("getting host key", zap.Error(err))
		return
	}

	ok, err := validateSignature(pk.GetPublic(), st)
	if err != nil {
		a.l.Error("validating share token", zap.Error(err))
		return
	} else if !ok {
		a.l.Error("invalid share token")
		return
	}

	tctx, tCancel := context.WithTimeout(ctx, 30*time.Second)
	defer tCancel()

	confirmFields := []otter.UIConfirmKV{
		{Label: "Receive file from", Value: s.Conn().RemotePeer().String()},
		{Label: "File size", Value: fmt.Sprintf("%d bytes", req.Len)},
	}

	if req.Name != "" {
		confirmFields = append(confirmFields, otter.UIConfirmKV{Label: "File name", Value: req.Name})
	}

	confirm := a.o.UI().Confirm(tctx, confirmFields)

	select {
	case confirmOk := <-confirm:
		if !confirmOk {
			return
		}
	case <-tctx.Done():
		return
	}

	//TODO(tcfw) the rest
}

func (a *FileDropHandler) GenerateShareToken(sender *peer.ID, expire time.Time) (string, error) {
	st := &ShareToken{
		Expires: expire.Format(time.RFC3339),
		Dest:    peer.ID(a.o.HostID()),
	}

	if sender != nil {
		st.Sender = *sender
	}

	sigData, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("marshalling share token for signature: %w", err)
	}

	pk, err := a.o.Crypto().HostKey()
	if err != nil {
		return "", fmt.Errorf("getting host key: %w", err)
	}

	sig, err := pk.Sign(sigData)
	if err != nil {
		return "", fmt.Errorf("signing share token: %w", err)
	}

	st.Signature = base64.RawStdEncoding.EncodeToString(sig)

	signedSt, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("marshalling signed share token: %w", err)
	}

	return base64.RawStdEncoding.EncodeToString(signedSt), nil
}

func validateSignature(k crypto.PubKey, t *ShareToken) (bool, error) {
	expr, err := time.Parse(time.RFC3339, t.Expires)
	if err != nil {
		return false, fmt.Errorf("parsing expire time: %w", err)
	}
	if time.Now().After(expr) {
		return false, errors.New("token expired")
	}

	token := *t
	token.Signature = ""

	sig, err := base64.RawStdEncoding.DecodeString(t.Signature)
	if err != nil {
		return false, fmt.Errorf("decoding signature: %w", err)
	}

	b, err := json.Marshal(token)
	if err != nil {
		return false, fmt.Errorf("marshalling share token: %w", err)
	}

	return k.Verify(b, sig)
}
