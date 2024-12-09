package api

import "github.com/tcfw/otter/pkg/id"

type OAuthTokenRequest struct {
	PublicID id.PublicID `json:"publicID"`
	Password string      `json:"password"`
}
