package internal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/tcfw/otter/internal/kek"
	"github.com/tcfw/otter/pkg/config"
)

const (
	dekSize = 32
)

func (o *Otter) HostKey() (crypto.PrivKey, error) {
	cv := o.GetConfig(config.P2P_HostKey)
	if cv == nil {
		//Generate
		priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generating new host key: %w", err)
		}

		pb, err := crypto.MarshalPrivateKey(priv)
		if err != nil {
			return nil, fmt.Errorf("generating new host key: %w", err)
		}

		if err := o.SetAndStoreConfig(config.P2P_HostKey, hex.EncodeToString(pb)); err != nil {
			return nil, fmt.Errorf("storing new host key: %w", err)
		}

		return priv, nil
	}

	hexVal, ok := cv.(string)
	if !ok {
		return nil, errUnsupportedSettingType
	}

	v, err := hex.DecodeString(hexVal)
	if err != nil {
		return nil, fmt.Errorf("parsing host key: %w", err)
	}

	return crypto.UnmarshalPrivateKey(v)
}

func (o *Otter) DiskKey() ([]byte, error) {
	sealedDEK := o.GetConfigAs("", config.Storage_SealedDEK).(string)
	if sealedDEK == "" {
		k, err := newDEK()
		if err != nil {
			return nil, fmt.Errorf("creating new DEK: %w", err)
		}
		sealedDEK, err := kek.SealKey(k)
		if err != nil {
			return nil, fmt.Errorf("sealing DEK for storage: %w", err)
		}
		err = o.SetAndStoreConfig(config.Storage_SealedDEK, hex.EncodeToString(sealedDEK))
		if err != nil {
			return nil, fmt.Errorf("storing sealing DEK: %w", err)
		}
		return k, nil
	}

	rDEK, err := hex.DecodeString(sealedDEK)
	if err != nil {
		return nil, fmt.Errorf("decoding sealed DEK: %w", err)
	}

	k, err := kek.UnsealKey(rDEK)
	if err != nil {
		return nil, fmt.Errorf("unsealing DEK: %w", err)
	}

	return k, nil
}

func newDEK() ([]byte, error) {
	dek := make([]byte, dekSize)
	if n, err := rand.Read(dek); err != nil || n != dekSize {
		return nil, fmt.Errorf("reading crypto for DEK: %w", err)
	}

	return dek, nil
}
