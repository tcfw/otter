package id

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	libp2pCrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-varint"
	"github.com/tyler-smith/go-bip39"
)

const (
	keyMultiBaseEncoding = multibase.Base58BTC
)

type PublicID string

type PrivateKey string

type publicableCrypto interface {
	Public() crypto.PublicKey
}

func (pubk PublicID) AsLibP2P() (libp2pCrypto.PubKey, error) {
	dc, err := DecodeCryptoMaterial(string(pubk))
	if err != nil {
		return nil, fmt.Errorf("decoding public key: %w", err)
	}

	switch pkt := dc.(type) {
	case ed25519.PublicKey:
		return libp2pCrypto.UnmarshalEd25519PublicKey(pkt)
	default:
		return nil, fmt.Errorf("unsupported key type: %T", pkt)
	}
}

func (pubk PublicID) AsPEM() (string, error) {
	pk, err := DecodeCryptoMaterial(string(pubk))
	if err != nil {
		return "", err
	}

	der, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		return "", err
	}

	p := pem.EncodeToMemory(&pem.Block{Bytes: der, Type: "PUBLIC KEY"})
	return string(p), nil
}

// PublicKey returns the encoded public key
func (pk PrivateKey) PublicKey() (PublicID, error) {
	rawPK, err := DecodeCryptoMaterial(string(pk))
	if err != nil {
		return "", fmt.Errorf("decoding private key: %w", err)
	}

	cpk, ok := rawPK.(publicableCrypto)
	if !ok {
		return "", errors.New("cannot get public key from private key")
	}

	pubk, err := EncodeCryptoMaterial(cpk.Public())
	if err != nil {
		return "", fmt.Errorf("encoding public key: %w", err)
	}

	return PublicID(pubk), nil
}

// EncodeCryptoMaterial encodes a public or private key in a did:key format:
// <multibase encoding><multicodec type><raw or encoded public/private key>
func EncodeCryptoMaterial(cm any) (string, error) {
	var mcc multicodec.Code
	var raw []byte

	switch cmt := cm.(type) {
	case ed25519.PrivateKey:
		mcc = multicodec.Ed25519Priv
		raw = []byte(cmt)
	case ed25519.PublicKey:
		mcc = multicodec.Ed25519Pub
		raw = []byte(cmt)
	default:
		return "", fmt.Errorf("unsupported crypto material type: %T", cmt)
	}

	vi := varint.ToUvarint(uint64(mcc))
	vi = append(vi, raw...)

	encoder := multibase.MustNewEncoder(keyMultiBaseEncoding)
	return encoder.Encode(vi), nil
}

// EncodeCryptoMaterial encodes a public or private key in a did:key format:
// <multibase encoding><multicodec type><raw or encoded public/private key>
func DecodeCryptoMaterial(cm string) (any, error) {
	_, mbd, err := multibase.Decode(cm)
	if err != nil {
		return nil, fmt.Errorf("decoding multibase wrapper: %w", err)
	}

	rawCodec, read, err := varint.FromUvarint(mbd)
	if err != nil {
		return nil, fmt.Errorf("decoding uvarint: %w", err)
	}

	raw := mbd[read:]

	switch codec := multicodec.Code(rawCodec); codec {
	case multicodec.Ed25519Pub:
		return ed25519.PublicKey(raw), nil
	case multicodec.Ed25519Priv:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("known codec: %v", codec)
	}
}

// RecoverKey creates a new Ed25519 pub/priv key from the given
// BIP39 mnemonic and password and returns the encoded public/private
// keys
func RecoverKey(mnemonic, pk string) (PublicID, PrivateKey, error) {
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, pk)
	if err != nil {
		return "", "", fmt.Errorf("validating seed: %w", err)
	}
	edPk := ed25519.NewKeyFromSeed(seed[:ed25519.SeedSize])

	pkenc, err := EncodeCryptoMaterial(edPk)
	if err != nil {
		return "", "", fmt.Errorf("encoding pk: %w", err)
	}
	pubenc, err := EncodeCryptoMaterial(edPk.Public().(ed25519.PublicKey))
	if err != nil {
		return "", "", fmt.Errorf("encoding pubk: %w", err)
	}

	return PublicID(pubenc), PrivateKey(pkenc), nil
}

// NewKey creates the entropy for a brand new key given the password
// and provides the encoded public/private keys with the generated
// BIP39 mnemonic words
func NewKey(pk string) (string, PublicID, PrivateKey, error) {
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return "", "", "", fmt.Errorf("creating entropy: %w", err)
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", "", "", fmt.Errorf("creating mnemonic: %w", err)
	}

	pubk, privk, err := RecoverKey(mnemonic, pk)
	if err != nil {
		return "", "", "", err
	}

	return mnemonic, pubk, privk, nil
}

func Sign(epk PrivateKey, data []byte, hasher crypto.Hash) ([]byte, error) {
	pk, err := DecodeCryptoMaterial(string(epk))
	if err != nil {
		return nil, fmt.Errorf("decoding key: %w", err)
	}

	switch p := pk.(type) {
	case ed25519.PrivateKey:
		sig, err := p.Sign(rand.Reader, data, hasher)
		if err != nil {
			return nil, fmt.Errorf("signing data:%w", err)
		}

		return sig, nil
	default:
		return nil, fmt.Errorf("known key type: %T", p)
	}
}
