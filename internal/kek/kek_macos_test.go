//go:build macos || darwin

package kek

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanUp(t *testing.T) {
	if err := deleteKeyChainSealingKey(true); err != nil {
		t.Fatal(err)
	}
}

func TestSealKey(t *testing.T) {
	plaintextValue := []byte("test")

	cipherText, err := SealKey(plaintextValue)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEqual(t, plaintextValue, cipherText)

	_, err = getKeyChainSealingKey()
	if err != nil {
		//should have stored the key in the keychain
		t.Fatal(err)
	}

	if err := deleteKeyChainSealingKey(true); err != nil {
		t.Fatal(err)
	}
}

func TestUnsealKey(t *testing.T) {
	plaintextValue := []byte("test")

	cipherText, err := SealKey(plaintextValue)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEqual(t, plaintextValue, cipherText)

	unsealedPlaintext, err := UnsealKey(cipherText)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, plaintextValue, unsealedPlaintext)

	if err := deleteKeyChainSealingKey(true); err != nil {
		t.Fatal(err)
	}
}
