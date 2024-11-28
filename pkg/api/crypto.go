package api

import "github.com/tcfw/otter/pkg/id"

type NewKeyRequest struct {
	Password string `json:"password"`
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
