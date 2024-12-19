package api

import crdt "github.com/ipfs/go-ds-crdt"

type SyncStatsResponse struct {
	Public  crdt.Stats `json:"public"`
	Private crdt.Stats `json:"private"`
}
