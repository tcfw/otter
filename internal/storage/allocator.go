package storage

import (
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/tcfw/otter/internal/metrics"
)

const (
	minLastCheckin = 1 * time.Hour
)

type flatPeerSet struct {
	peer peer.ID
	set  metrics.Set
}

type PeerSorter interface {
	sort.Interface

	SetPeers([]flatPeerSet)
	GetPeers() []flatPeerSet
}

func OrderPeersBy(pcs metrics.PeerCollectorLastSet, s PeerSorter) []peer.ID {
	peers := []flatPeerSet{}
	for p, c := range pcs {
		if c.Ts.Before(time.Now().Add(-1*minLastCheckin)) || len(c.Set) == 0 {
			continue
		}

		peers = append(peers, flatPeerSet{peer: p, set: c.Set})
	}

	s.SetPeers(peers)
	sort.Sort(s)

	sortedPeers := []peer.ID{}
	for _, p := range s.GetPeers() {
		sortedPeers = append(sortedPeers, p.peer)
	}

	return sortedPeers
}

type AvailableSpace struct {
	peers []flatPeerSet
}

func (bas *AvailableSpace) SetPeers(p []flatPeerSet) {
	bas.peers = p
}

func (bas *AvailableSpace) GetPeers() []flatPeerSet {
	return bas.peers
}

func (bas *AvailableSpace) Len() int {
	return len(bas.peers)
}

func (bas *AvailableSpace) Swap(i, j int) {
	bas.peers[i], bas.peers[j] = bas.peers[j], bas.peers[i]
}

func (bas *AvailableSpace) Less(i, j int) bool {
	s1 := bas.peers[i].set[metrics.SetKeyDiskAvail].(int64)
	s2 := bas.peers[j].set[metrics.SetKeyDiskAvail].(int64)
	return s1 < s2
}

type UsedSpace struct {
	peers []flatPeerSet
}

func (bas *UsedSpace) SetPeers(p []flatPeerSet) {
	bas.peers = p
}

func (bas *UsedSpace) GetPeers() []flatPeerSet {
	return bas.peers
}

func (bas *UsedSpace) Len() int {
	return len(bas.peers)
}

func (bas *UsedSpace) Swap(i, j int) {
	bas.peers[i], bas.peers[j] = bas.peers[j], bas.peers[i]
}

func (bas *UsedSpace) Less(i, j int) bool {
	s1 := bas.peers[i].set[metrics.SetKeyDiskAvail].(int64)
	t1 := bas.peers[i].set[metrics.SetKeyDiskTotal].(int64)
	s2 := bas.peers[j].set[metrics.SetKeyDiskAvail].(int64)
	t2 := bas.peers[j].set[metrics.SetKeyDiskTotal].(int64)
	return (t1 - s1) < (t2 - s2)

}
