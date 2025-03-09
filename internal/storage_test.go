package internal

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	chunker "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	"github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/tcfw/otter/pkg/id"
)

func newMockDagService() *mockDagService {
	return &mockDagService{
		data: make(map[cid.Cid]ipld.Node),
	}
}

type mockDagService struct {
	data map[cid.Cid]ipld.Node
	mu   sync.RWMutex
}

func (g *mockDagService) Add(_ context.Context, n ipld.Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.data[n.Cid()] = n

	return nil
}

func (g *mockDagService) AddMany(_ context.Context, ns []ipld.Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, n := range ns {
		g.data[n.Cid()] = n
	}

	return nil
}

func (g *mockDagService) Remove(_ context.Context, c cid.Cid) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.data, c)

	return nil
}

func (g *mockDagService) RemoveMany(_ context.Context, cs []cid.Cid) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, c := range cs {
		delete(g.data, c)
	}

	return nil
}

func (g *mockDagService) Get(_ context.Context, c cid.Cid) (ipld.Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	n, ok := g.data[c]
	if ok {
		return n, nil
	}

	return nil, ipld.ErrNotFound{Cid: c}
}

func (g *mockDagService) GetMany(ctx context.Context, cs []cid.Cid) <-chan *ipld.NodeOption {
	out := make(chan *ipld.NodeOption, len(cs))

	go func() {
		defer close(out)

		for _, c := range cs {
			n, err := g.Get(ctx, c)
			out <- &ipld.NodeOption{Node: n, Err: err}
		}
	}()

	return out
}

func TestDagDecryptingNodeGetter(t *testing.T) {
	ds := newMockDagService()

	_, pubk, privk, err := id.NewKey("")
	if err != nil {
		t.Fatal(err)
	}

	inputData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}

	//Adder
	prefix, err := merkledag.PrefixForCidVersion(1)
	if err != nil {
		t.Fatalf("bad CID Version: %s", err)
	}

	prefix.MhType = multihash.SHA2_256
	prefix.MhLength = -1

	dbp := helpers.DagBuilderParams{
		Dagserv:    ds,
		Maxlinks:   helpers.DefaultLinksPerBlock,
		CidBuilder: &prefix,
	}

	chnk, err := chunker.FromString(bytes.NewReader(inputData), "size-10")
	if err != nil {
		t.Fatal(err)
	}

	chnk, err = newEncryptedChunker(chnk, privk)
	if err != nil {
		t.Fatal(err)
	}

	dbh, err := dbp.New(chnk)
	if err != nil {
		t.Fatal(err)
	}

	n, err := balanced.Layout(dbh)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("cid", n.Cid().String())

	ad := []byte(pubk)

	sk, err := privateKeytoStorageKey(privk, nil)
	if err != nil {
		t.Fatalf("getting storage key: %s", err)
	}

	aead, err := privateStorageAEAD(sk)
	if err != nil {
		t.Fatalf("creating AEAD: %s", err)
	}

	r, err := NewEncryptedUnixFSReader(context.Background(), n, ds, aead, ad)
	if err != nil {
		t.Fatal(err)
	}

	defer r.Close()

	outputData := make([]byte, len(inputData))
	rn, err := r.Read(outputData)
	if err != nil {
		t.Fatal(err)
	}
	if rn != len(inputData) {
		t.Fatal(fmt.Errorf("failed to read back plaintext, got %d, expected %d", rn, len(inputData)))
	}

	assert.Equal(t, inputData, outputData)
}
