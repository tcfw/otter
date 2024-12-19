package api

import (
	"crypto"

	"github.com/tcfw/otter/pkg/id"
)

type NewKeyRequest struct {
	Password string `json:"password"`
}

type ImportKeyRequest struct {
	Password string `json:"password"`
	Mnemonic string `json:"mnemonic"`
}

type NewKeyResponse struct {
	Mnemonic string      `json:"mnemonic"`
	PublicID id.PublicID `json:"publicID"`
}

type KeyListResponse struct {
	Keys []id.PublicID `json:"keys"`
}

type DeleteKeyRequest struct {
	PublicID id.PublicID `json:"publicID"`
	Password string      `json:"password"`
}

type SignRequest struct {
	Data     []byte      `json:"data"`
	PublicID id.PublicID `json:"publicID"`
	HashID   crypto.Hash `json:"hashID"`
}

type SignResponse struct {
	Sig []byte `json:"sig"`
}
