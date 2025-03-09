package internal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSealUnseal(t *testing.T) {
	paead, err := privateStorageAEAD([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}

	test := []byte("test")
	ctx := context.Background()
	ct, err := privateStorageSeal(paead, nil)(ctx, test)
	if err != nil {
		t.Fatal(err)
	}

	pt, err := privateStorageUnseal(paead, nil)(ctx, ct)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, test, pt)
}
