package vrr

import (
	"log"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"
)

func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	rand.Seed(time.Now().UnixNano())
}

type Harness struct {
	mu sync.Mutex

	commitChans []chan CommitEntry
	commits     [][]CommitEntry

	cluster []*Server

	connected []bool

	n int
	t *testing.T
}

func NewHarness(t *testing.T, n int) *Harness {
	ns := make([]*Server, n)
	connected := make([]bool, n)
	commitChans := make([]chan CommitEntry, n)
	commits := make([][]CommitEntry, n)
	ready := make(chan interface{})

	for i := 0; i < n; i++ {
		commitChans[i] = make(chan CommitEntry)
		ns[i] = NewServer(ready, commitChans[i])
		ns[i].Serve()
	}

	sort.SliceStable(ns, func(i, j int) bool {
		return ns[i].GetListenAddr().String() < ns[j].GetListenAddr().String()
	})

	for i := 0; i < n; i++ {
		ns[i].serverID = i
		log.Printf("[id:%d] server listens at %s", ns[i].serverID, ns[i].GetListenAddr())

		// configuration will be a map of ReplicaID and TCP address
		// of other peer replicas.
		configuration := make(map[int]string)
		for j := 0; j < n; j++ {
			if j != i {
				// fmt.Println(ns[j].GetListenAddr().String())
				configuration[j] = ns[j].GetListenAddr().String()
				// fmt.Println(configuration)
			}
			err := ns[i].ConnectToPeer(j, ns[j].GetListenAddr())
			if err != nil {
				log.Fatalf("%d failed to connect with %d :(", i, j)
			}
		}
		ns[i].configuration = configuration

		ns[i].replica.ID = i
		ns[i].replica.configuration = configuration
		ns[i].replica.primaryID = 0

		connected[i] = true
	}
	close(ready)

	h := &Harness{
		commitChans: commitChans,
		commits:     commits,
		cluster:     ns,
		connected:   connected,
		n:           n,
		t:           t,
	}

	for i := 0; i < n; i++ {
		go h.collectCommits(i)
	}

	return h

}

func (h *Harness) Shutdown() {
	for i := 0; i < h.n; i++ {
		h.cluster[i].DisconnectAll()
		h.connected[i] = false
	}
	for i := 0; i < h.n; i++ {
		h.cluster[i].Shutdown()
	}
}

func (h *Harness) DisconnectPeer(ID int) {
	tlog("Disconnect %d", ID)
	h.cluster[ID].DisconnectAll()
	for j := 0; j < h.n; j++ {
		if j != ID {
			h.cluster[j].DisconnectPeer(ID)
		}
	}
	h.connected[ID] = false
}

func (h *Harness) ReconnectPeer(ID int) {
	tlog("Reconnect %d", ID)
	for j := 0; j < h.n; j++ {
		if j != ID {
			if err := h.cluster[ID].ConnectToPeer(j, h.cluster[j].GetListenAddr()); err != nil {
				h.t.Fatal(err)
			}
			if err := h.cluster[j].ConnectToPeer(ID, h.cluster[ID].GetListenAddr()); err != nil {
				h.t.Fatal(err)
			}
		}
	}
	h.connected[ID] = true
}

// CheckSinglePrimary returns primary's ID and viewNum.
func (h *Harness) CheckSinglePrimary() (int, int) {
	return 0, 0
}

func (h *Harness) CheckNoPrimary() {

}

func tlog(format string, a ...interface{}) {
	format = "[TEST] " + format
	log.Printf(format, a...)
}

func sleepMs(n int) {
	time.Sleep(time.Duration(n) * time.Millisecond)
}

func (h *Harness) collectCommits(i int) {
	for c := range h.commitChans[i] {
		h.mu.Lock()
		tlog("collectCommits(%d) got %+v", i, c)
		h.commits[i] = append(h.commits[i], c)
		h.mu.Unlock()
	}
}
