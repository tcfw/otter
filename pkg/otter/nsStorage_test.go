package otter

import (
	"testing"

	"github.com/ipfs/go-datastore"
	"github.com/zeebo/assert"
)

func TestTrimPrefixDatasetKey(t *testing.T) {
	res := trimNamespacePrefix(datastore.NewKey("pins"), datastore.NewKey("/pins/p/test"))
	assert.Equal(t, datastore.NewKey("/p/test"), res)
}
