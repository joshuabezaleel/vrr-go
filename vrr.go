package vrr

import (
	"fmt"
	"log"
	"sync"
)

type ReplicaStatus int

const (
	Normal ReplicaStatus = iota
	Recovery
	ViewChange
	Transitioning
	Dead
)

func (rs ReplicaStatus) String() string {
	switch rs {
	case Normal:
		return "Normal"
	case Recovery:
		return "Recovery"
	case ViewChange:
		return "View-Change"
	case Transitioning:
		return "Transitioning"
	case Dead:
		return "Dead"
	default:
		panic("unreachable")
	}
}

type Replica struct {
	mu sync.Mutex

	ID int

	status ReplicaStatus

	configuration map[int]string

	server *Server
}

func NewReplica(ID int, configuration map[int]string, server *Server, ready <-chan interface{}) *Replica {
	replica := new(Replica)
	replica.ID = ID
	replica.configuration = configuration
	replica.server = server

	replica.status = Normal

	// go func() {
	// 	<-ready
	// 	replica.mu.Lock()
	// 	defer replica.mu.Unlock()
	// 	replica.greetOthers()
	// }()

	return replica
}

func (r *Replica) dlog(format string, args ...interface{}) {
	format = fmt.Sprintf("[%d] ", r.ID) + format
	log.Printf(format, args...)
}

type HelloArgs struct {
	ID int
}

type HelloReply struct {
	ID int
}

func (r *Replica) Hello(args HelloArgs, reply *HelloReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == Dead {
		return nil
	}
	reply.ID = r.ID
	r.dlog("%d receive the greetings from %d! :)", reply.ID, args.ID)
	return nil
}

// func (r *Replica) startSayingHi() {
// 	r.greetOthers()
// }

func (r *Replica) greetOthers() {
	for peerID := range r.configuration {
		args := HelloArgs{
			ID: r.ID,
		}

		go func(peerID int) {
			r.dlog("%d is trying to say hello to %d!", r.ID, peerID)
			var reply HelloReply
			if err := r.server.Call(peerID, "Replica.Hello", args, &reply); err == nil {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.dlog("%d says hi back to %d!! yay!", reply.ID, r.ID)
				return
			}
		}(peerID)
	}
}
