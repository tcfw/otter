package id

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewKey(t *testing.T) {
	pass := "test"

	mn, pubk, privk, err := NewKey(pass)
	if err != nil {
		t.Fatal(err)
	}

	pubk2, privk2, err := RecoverKey(mn, pass)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pubk, pubk2)
	assert.Equal(t, privk, privk2)

	pk, err := DecodeCryptoMaterial(string(privk))
	if err != nil {
		t.Fatal(err)
	}
	assert.IsType(t, ed25519.PrivateKey{}, pk)
}

func TestPublicKeyFromPrivate(t *testing.T) {
	_, pubk, privk, err := NewKey("")
	if err != nil {
		t.Fatal(err)
	}

	pubk2, err := privk.PublicKey()
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pubk, pubk2)
}
