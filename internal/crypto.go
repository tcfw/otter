package internal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/tcfw/otter/pkg/config"
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
