package dht

import (
	"bytes"
	"sort"
)

const (
	bucketSize = 8
)

type routingTable struct {
	self    NodeID
	buckets [][]Node
}

func newRoutingTable(self NodeID) *routingTable {
	return &routingTable{
		self:    self,
		buckets: make([][]Node, 160),
	}
}

func (rt *routingTable) add(n Node) {
	if bytes.Equal(n.id[:], rt.self[:]) {
		return
	}
	i := bucketIndex(rt.self, n.id)
	b := rt.buckets[i]
	// check for existing
	for idx := range b {
		if bytes.Equal(b[idx].id[:], n.id[:]) {
			return
		}
	}
	if len(b) < bucketSize {
		rt.buckets[i] = append(b, n)
		return
	}
	// simple eviction: drop oldest (front)
	rt.buckets[i] = append(b[1:], n)
}

func (rt *routingTable) nearest(target NodeID, count int) []Node {
	all := make([]Node, 0, bucketSize*len(rt.buckets))
	for _, b := range rt.buckets {
		all = append(all, b...)
	}
	sort.Slice(all, func(i, j int) bool {
		return distanceLess(all[i].id, all[j].id, target)
	})
	if len(all) > count {
		all = all[:count]
	}
	return all
}

func bucketIndex(a, b NodeID) int {
	for i := 0; i < 20; i++ {
		x := a[i] ^ b[i]
		if x == 0 {
			continue
		}
		for bit := 0; bit < 8; bit++ {
			if x&(0x80>>bit) != 0 {
				return i*8 + bit
			}
		}
	}
	return 159
}

func distanceLess(a, b, target NodeID) bool {
	for i := 0; i < 20; i++ {
		da := a[i] ^ target[i]
		db := b[i] ^ target[i]
		if da == db {
			continue
		}
		return da < db
	}
	return false
}
