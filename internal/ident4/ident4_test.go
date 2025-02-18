package ident4

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tcfw/otter/internal/mocks"
	"github.com/tcfw/otter/pkg/id"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	mocknetwork "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"go.uber.org/zap"
)

func TestHandshake(t *testing.T) {
	mn, err := mocknetwork.FullMeshConnected(2)
	if err != nil {
		t.Fatal(err)
	}
	defer mn.Close()

	h1, h2 := mn.Hosts()[0], mn.Hosts()[1]

	logger := zap.NewNop()

	ks1 := mocks.NewCryptography(nil)
	ks2 := mocks.NewCryptography(nil)

	if err := Setup(h1, logger, ks1); err != nil {
		t.Fatal(err)
	}

	i4h1 := defaultClient

	if err := Setup(h2, logger, ks2); err != nil {
		t.Fatal(err)
	}

	// i4h2 := defaultClient

	ping.NewPingService(h2)

	_, pub1, priv1, err := id.NewKey("")
	if err != nil {
		t.Fatal(err)
	}

	_, pub2, priv2, err := id.NewKey("")
	if err != nil {
		t.Fatal(err)
	}

	if err := ks1.KeyStore().(*mocks.MockKeyStore).AddKey(priv1); err != nil {
		t.Fatal(err)
	}

	if err := ks2.KeyStore().(*mocks.MockKeyStore).AddKey(priv2); err != nil {
		t.Fatal(err)
	}

	s, err := i4h1.DialContext(context.Background(), h2.ID(), protocol.ID(ping.ID), pub2, pub1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const data = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	if _, err := s.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(data))
	if _, err := s.Read(buf); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, string(buf[:]), data)

	assert.Equal(t, s.Conn().LocalPeer(), peer.ID(pub1))
	assert.Equal(t, s.Conn().RemotePeer(), peer.ID(pub2))
}
