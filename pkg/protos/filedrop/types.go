package filedrop

import "github.com/libp2p/go-libp2p/core/peer"

type ShareToken struct {
	Dest      peer.ID `json:"dest"`
	Sender    peer.ID `json:"sender,omitempty"`
	Expires   string  `json:"expires"`
	Signature string  `json:"sig"`
}

type TransferType string

const (
	TransferType_Direct = "direct"
	TransferType_CID    = "cid"
)

type TransferRequest struct {
	ShareToken string       `json:"token"`
	Type       TransferType `json:"type"`
	Name       string       `json:"filename"`
	Len        uint64       `json:"len"`
	CID        string       `json:"cid"`
}

type TransferAck struct {
	Ack bool `json:"ack"`
}
