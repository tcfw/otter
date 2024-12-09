package activitypub

import (
	"testing"

	"github.com/ipfs/go-cid"
)

func TestDecodeCID(t *testing.T) {
	rawCid := "bagaaiera4ypifvajg7qe7tvcpkm3pvcwblep2s2bwdajnavavivra6ml4uta"
	c, err := cid.Decode(rawCid)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(c)
}
