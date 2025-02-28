package metrics

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tcfw/otter/pkg/config"
)

func TestCollect(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cv := config.GetConfig(config.Storage_Dir)
	config.SetConfig(config.Storage_Dir, dir)
	t.Cleanup(func() {
		config.SetConfig(config.Storage_Dir, cv)
	})

	set, err := Collect()
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, set)
}
