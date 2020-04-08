package vrr

import (
	"log"
	"math/rand"
	"sort"
	"testing"
	"time"
)

func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	rand.Seed(time.Now().UnixNano())
}

type Harness struct {
	cluster []*Server

	connected []bool

	n int
	t *testing.T
}

func NewHarness(t *testing.T, n int) *Harness {
	ns := make([]*Server, n)
	connected := make([]bool, n)
	ready := make(chan interface{})

	for i := 0; i < n; i++ {
		ns[i] = NewServer(ready)
		ns[i].Serve()
	}

	sort.SliceStable(ns, func(i, j int) bool {
		return ns[i].GetListenAddr().String() < ns[j].GetListenAddr().String()
	})

	for i := 0; i < n; i++ {
		ns[i].serverID = i
		log.Printf("[id:%d] server listens at %s", i, ns[i].GetListenAddr())

		configuration := make(map[int]string)
		for j := 0; j < n; j++ {
			if j != i {
				configuration[j] = ns[j].GetListenAddr().String()
				ns[j].configuration = configuration
			}
		}
	}

	return &Harness{
		cluster:   ns,
		connected: connected,
		n:         n,
		t:         t,
	}
}

func (h *Harness) Shutdown() {
	// for i := 0; i < h.n; i++ {
	// 	h.cluster[i].DisconnectAll()
	// 	h.connected[i] = false
	// }
	for i := 0; i < h.n; i++ {
		h.cluster[i].Shutdown()
	}
}
