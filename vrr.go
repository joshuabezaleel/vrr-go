package vrr

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
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

	configuration map[int]string

	server *Server

	oldViewNum int
	viewNum    int
	commitNum  int
	opNum      int
	opLog      []interface{}
	primaryID  int

	doViewChangeCount int

	status               ReplicaStatus
	viewChangeResetEvent time.Time
}

func NewReplica(ID int, configuration map[int]string, server *Server, ready <-chan interface{}) *Replica {
	replica := new(Replica)
	replica.ID = ID
	replica.configuration = configuration
	replica.server = server
	replica.oldViewNum = -1
	replica.doViewChangeCount = 0

	replica.status = Normal

	go func() {
		<-ready
		replica.mu.Lock()
		replica.viewChangeResetEvent = time.Now()
		replica.mu.Unlock()
		replica.runViewChangeTimer()
	}()

	return replica
}

func (r *Replica) dlog(format string, args ...interface{}) {
	format = fmt.Sprintf("[%d] ", r.ID) + format
	log.Printf(format, args...)
}

func (r *Replica) runViewChangeTimer() {
	timeoutDuration := time.Duration(150+rand.Intn(150)) * time.Millisecond
	r.mu.Lock()
	viewStarted := r.viewNum
	r.mu.Unlock()
	r.dlog("view change timer started (%v), view=%d", timeoutDuration, viewStarted)

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		<-ticker.C

		r.mu.Lock()

		if r.status == ViewChange {
			r.dlog("status become View-Change, blast <START-VIEW-CHANGE> to all replicas")
			r.doViewChangeCount = 0
			r.blastStartViewChange()

			// if r.doViewChangeCount != 0 {
			// 	r.dlog("ASD?")
			// }

			r.mu.Unlock()
			return
		}

		// if r.status == ViewChange && r.doViewChangeCount != 0 {
		// 	r.dlog("MASUK DONG")
		// 	return
		// }

		if elapsed := time.Since(r.viewChangeResetEvent); elapsed >= timeoutDuration {
			r.initiateViewChange()
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
	}
}

func (r *Replica) blastStartViewChange() {
	savedCurrentViewNum := r.viewNum
	var repliesReceived int32 = 1
	var sendStartViewChangeAlready bool = false

	for peerID := range r.configuration {
		go func(peerID int) {
			args := StartViewChangeArgs{
				ViewNum:   savedCurrentViewNum,
				ReplicaID: r.ID,
			}
			var reply StartViewChangeReply

			r.dlog("sending <START-VIEW-CHANGE> to %d: %+v", peerID, args)
			err := r.server.Call(peerID, "Replica.StartViewChange", args, &reply)
			if err != nil {
				log.Println(err)
			}
			if err == nil {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.dlog("received <START-VIEW-CHANGE> reply +%v", reply)

				if reply.IsReplied && !sendStartViewChangeAlready {
					replies := int(atomic.AddInt32(&repliesReceived, 1))
					if replies*2 > len(r.configuration)+1 {
						r.dlog("acknowledge that quorum agrees on a view change. Sending <DO-VIEW-CHANGE> to new designated primary")
						r.sendDoViewChange()
						sendStartViewChangeAlready = true
						return
					}
				}
			}
		}(peerID)
	}
}

func (r *Replica) sendDoViewChange() {
	nextPrimaryID := nextPrimary(r.primaryID, r.configuration)
	// newViewNum := r.viewNum

	args := DoViewChangeArgs{
		ViewNum:    r.viewNum,
		OldViewNum: r.oldViewNum,
		CommitNum:  r.commitNum,
		OpNum:      r.opNum,
		OpLog:      r.opLog,
	}
	var reply DoViewChangeReply

	r.dlog("sending <DO-VIEW-CHANGE> to the next primary %d: %+v", nextPrimaryID, args)
	err := r.server.Call(nextPrimaryID, "Replica.DoViewChange", args, &reply)
	if err == nil {
		r.dlog("received <DO-VIEW-CHANGE> reply +%v", reply)
		return
	}
}

func (r *Replica) initiateViewChange() {
	r.status = ViewChange
	r.viewNum += 1
	savedCurrentViewNum := r.viewNum
	r.viewChangeResetEvent = time.Now()
	r.dlog("initiates VIEW CHANGE; view=%d; log=<ADDED LATER>", savedCurrentViewNum)

	// Run another ViewChangeTimer in case this ViewChange is failed.
	go r.runViewChangeTimer()
}

func (r *Replica) blastStartView() {
	r.dlog("BLAST START VIEW")
}

type DoViewChangeArgs struct {
	ViewNum    int
	OldViewNum int
	CommitNum  int
	OpNum      int
	OpLog      []interface{}
}

type DoViewChangeReply struct {
	IsReplied bool
	ReplicaID int
}

func (r *Replica) DoViewChange(args DoViewChangeArgs, reply *DoViewChangeReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status == Dead {
		return nil
	}
	r.dlog("DoViewChange: %+v [currentView=%d]", args, r.viewNum)

	r.doViewChangeCount++
	r.dlog("DoViewChange messages received: %d", r.doViewChangeCount)

	if r.doViewChangeCount > (len(r.configuration)/2)+1 {
		r.dlog("quorum")
		// TODO
		// Comparing messages to other replicas' data and taking the newest.
		r.commitNum = args.CommitNum
		r.dlog("commitNum = %v", r.commitNum)
		r.blastStartView()
	}

	return nil
}

type StartViewChangeArgs struct {
	ViewNum   int
	ReplicaID int
}

type StartViewChangeReply struct {
	IsReplied bool
	ReplicaID int
}

func (r *Replica) StartViewChange(args StartViewChangeArgs, reply *StartViewChangeReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status == Dead {
		return nil
	}
	r.dlog("StartViewChange: %+v [currentView=%d]", args, r.viewNum)

	// If the incoming <START-VIEW-CHANGE> message got a bigger `view-num`
	// than the one that the replica has.
	if args.ViewNum > r.viewNum {
		// Set status to `view-change`, set `view-num` to the message's `view-num`
		// and reply with <START-VIEW-CHANGE> to all replicas.
		reply.IsReplied = true
		reply.ReplicaID = r.ID
		r.status = ViewChange
		r.oldViewNum = r.viewNum
		r.viewNum = args.ViewNum
		r.viewChangeResetEvent = time.Now()
	} else if args.ViewNum == r.viewNum {
		reply.IsReplied = true
		reply.ReplicaID = r.ID
	}
	r.dlog("... StartViewChange replied: %+v", reply)
	return nil
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
	r.dlog("%d receive the greetings from %d! :)", reply.ID, args.ID)
	reply.ID = r.ID
	return nil
}

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

func nextPrimary(primaryID int, config map[int]string) int {
	nextPrimaryID := primaryID + 1
	if nextPrimaryID == len(config)+1 {
		nextPrimaryID = 0
	}

	return nextPrimaryID
}
