package mocks

import (
	"context"
	"crypto"
	"errors"

	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/keystore"
	"github.com/tcfw/otter/pkg/otter"

	libp2pCrypto "github.com/libp2p/go-libp2p/core/crypto"
)

func NewKeyStore() keystore.KeyStore {
	return &MockKeyStore{keys: make(map[id.PublicID]id.PrivateKey)}
}

type MockKeyStore struct {
	keys map[id.PublicID]id.PrivateKey
}

func (ks *MockKeyStore) Keys(ctx context.Context) ([]id.PublicID, error) {
	a := make([]id.PublicID, 0, len(ks.keys))

	for k := range ks.keys {
		a = append(a, k)
	}

	return a, nil
}

func (ks *MockKeyStore) Sign(ctx context.Context, pub id.PublicID, d []byte, h crypto.Hash) ([]byte, error) {
	pk, ok := ks.keys[pub]
	if !ok {
		return nil, errors.New("not found")
	}

	sig, err := id.Sign(pk, d, h)
	if err != nil {
		return nil, err
	}

	return sig, nil
}

func (ks *MockKeyStore) PrivateSeal(ctx context.Context, pub id.PublicID, d []byte) ([]byte, error) {
	if _, ok := ks.keys[pub]; !ok {
		return nil, errors.New("not found")
	}

	return d, nil
}

func (ks *MockKeyStore) PrivateUnseal(ctx context.Context, pub id.PublicID, d []byte) ([]byte, error) {
	if _, ok := ks.keys[pub]; !ok {
		return nil, errors.New("not found")
	}

	return d, nil
}

func (ks *MockKeyStore) AddKey(pk id.PrivateKey) error {
	pubk, err := pk.PublicKey()
	if err != nil {
		return err
	}

	ks.keys[pubk] = pk

	return nil
}

func NewCryptography(privk libp2pCrypto.PrivKey) otter.Cryptography {
	return &MockCrypto{privk: privk, ks: NewKeyStore()}
}

type MockCrypto struct {
	privk libp2pCrypto.PrivKey
	ks    keystore.KeyStore
}

// Returns the hode nodes private key
func (m *MockCrypto) HostKey() (libp2pCrypto.PrivKey, error) {
	return m.privk, nil
}

// Identity key store
func (m *MockCrypto) KeyStore() keystore.KeyStore {
	return m.ks
}
