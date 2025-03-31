package petnames

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ipfs/go-datastore"
	badger3 "github.com/ipfs/go-ds-badger3"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	mocknetwork "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/tcfw/otter/internal/ident4"
	"github.com/tcfw/otter/internal/mocks"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/protos/petnames/pb"
	testmodels "github.com/tcfw/otter/pkg/protos/petnames/test_models"
	"go.uber.org/zap"
)

func TestBarabasiAlbert(t *testing.T) {
	n := 100 // Number of nodes
	m := 5   // Number of edges to add per new node
	n0 := 8  // Initial number of connected nodes

	socnet := testmodels.BarabasiAlbert(n, m, n0)

	mn, err := mocknetwork.FullMeshConnected(n)
	if err != nil {
		t.Fatal(err)
	}
	defer mn.Close()

	logger := zap.NewNop()

	kss := make([]otter.Cryptography, 0, n)
	pbs := make([]id.PublicID, 0, n)
	ois := make([]otter.Otter, 0, n)
	pnhs := make([]*PetnamesHandler, 0, n)

	dir, err := os.MkdirTemp("", "otter-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	baseDs, err := badger3.NewDatastore(dir, &badger3.DefaultOptions)
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range mn.Hosts() {
		ks := mocks.NewCryptography(nil)

		i4, err := ident4.Setup(h, logger, ks)
		if err != nil {
			t.Fatal(err)
		}

		_, pub, priv, err := id.NewKey("")
		if err != nil {
			t.Fatal(err)
		}

		if err := ks.KeyStore().(*mocks.MockKeyStore).AddKey(priv); err != nil {
			t.Fatal(err)
		}

		mockOtter := mocks.NewMockOtter(t)
		mockOtter.EXPECT().HostID().Maybe().Return(h.ID())
		mockStorageClasses := mocks.NewMockStorageClasses(t)

		pubds, err := otter.NewNamespacedStorage(baseDs, datastore.KeyWithNamespaces([]string{h.ID().String(), "pub"}), logger)
		if err != nil {
			t.Fatal(err)
		}
		privds, err := otter.NewNamespacedStorage(baseDs, datastore.KeyWithNamespaces([]string{h.ID().String(), "priv"}), logger)
		if err != nil {
			t.Fatal(err)
		}

		mockStorageClasses.EXPECT().Public(mock.Anything).Maybe().Return(pubds, nil)
		mockStorageClasses.EXPECT().PrivateFromPublic(mock.Anything).Maybe().Return(privds, nil)
		mockOtter.EXPECT().Storage().Maybe().Return(mockStorageClasses)
		mockOtter.EXPECT().ResolveOtterNodesForKey(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, pub id.PublicID) ([]peer.ID, error) {
			for i, p := range pbs {
				if p == pub {
					return []peer.ID{ois[i].HostID()}, nil
				}
			}

			return nil, errors.New("not found")
		}).Maybe()

		mockProtos := mocks.NewMockProtocols(t)
		mockProtos.EXPECT().P2P().Maybe().Return(h)
		mockProtos.EXPECT().DialContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, peer peer.ID, proto protocol.ID, remote, local id.PublicID) (network.Stream, error) {
			return i4.DialContext(ctx, peer, proto, remote, local)
		}).Maybe()
		mockOtter.EXPECT().Protocols().Maybe().Return(mockProtos)

		pnh := &PetnamesHandler{mockOtter, logger}
		mockOtter.Protocols().P2P().SetStreamHandler(protoID, pnh.handle)

		kss = append(kss, ks)
		pbs = append(pbs, pub)
		ois = append(ois, mockOtter)
		pnhs = append(pnhs, pnh)
	}

	for n1, n1s := range socnet {
		for _, n2 := range n1s {
			sc, err := pnhs[n1].ForPublicID(pbs[n1])
			if err != nil {
				t.Fatal(err)
			}

			n := &pb.Name{FirstName: fmt.Sprintf("%d", n2)}

			sc.SetLocalContact(context.Background(), &pb.Contact{
				Id:          string(pbs[n2]),
				SharedName:  n,
				PrivateName: n,
			})
		}
	}

	var startHost int
	for n1, n1s := range socnet {
		if len(n1s) <= m {
			startHost = n1
		}
	}

	var targetPublicID id.PublicID
	for n1, n1s := range socnet {
		if len(n1s) <= m && !slices.Contains(n1s, startHost) {
			targetPublicID = pbs[n1]
		}
	}

	startScopedClient, err := pnhs[startHost].ForPublicID(pbs[startHost])
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	edgeNamesCh, err := startScopedClient.SearchForEdgeNames(ctx, targetPublicID)
	if err != nil {
		t.Fatal(err)
	}

	edgeNames := []*pb.DOSName{}
	for en := range edgeNamesCh {
		edgeNames = append(edgeNames, en)
		if len(edgeNames) >= 4 {
			cancel()
			break
		}
	}

	if len(edgeNames) < 1 {
		t.Fatal("did not find enough edge names")
	}
	assert.Equal(t, edgeNames[0], edgeNames[1])
}
