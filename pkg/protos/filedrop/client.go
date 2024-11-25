package filedrop

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type FileDropClient interface {
	GenerateShareToken(sender *peer.ID, expire time.Time) (string, error)
}

func Client() FileDropClient {
	return fdh
}
