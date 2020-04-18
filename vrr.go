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

	viewNum   int
	primaryID int

	status               ReplicaStatus
	viewChangeResetEvent time.Time
}

func NewReplica(ID int, configuration map[int]string, server *Server, ready <-chan interface{}) *Replica {
	replica := new(Replica)
	replica.ID = ID
	replica.configuration = configuration
	replica.server = server

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

		// IF START-VIEW-CHANGE >= f+1
		//
		// TO-DO
		if r.status == ViewChange {
			r.dlog("status become View-Change, blast <START-VIEW-CHANGE> to all replicas")
			r.blastStartViewChange()

			// TODO
			// if r.ID == r.primaryID {

			// }
			r.mu.Unlock()
			return
		}

		if elapsed := time.Since(r.viewChangeResetEvent); elapsed >= timeoutDuration {
			r.initiateViewChange()
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
	}
}

func (r *Replica) blastStartViewChange() {
	r.dlog("masuk")
	r.mu.Lock()
	savedCurrentViewNum := r.viewNum
	r.mu.Unlock()
	var repliesReceived int32 = 1

	for peerID := range r.configuration {
		go func(peerID int) {
			args := StartViewChangeArgs{
				viewNum:   savedCurrentViewNum,
				replicaID: r.ID,
			}
			var reply StartViewChangeReply

			r.dlog("sending <START-VIEW-CHANGE> to %d: %+v", peerID, args)
			if err := r.server.Call(peerID, "Replica.StartViewChange", args, &reply); err == nil {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.dlog("received <START-VIEW-CHANGE> reply +%v", reply)

				if reply.isReplied {
					replies := int(atomic.AddInt32(&repliesReceived, 1))
					if replies*2 > len(r.configuration)+1 {
						r.dlog("acknowledge that quorum agrees on a view change. Sending <DO-VIEW-CHANGE> to new designated primary")
						r.sendDoViewChange()
						return
					}
				}
			}
		}(peerID)
	}
}

func (r *Replica) sendDoViewChange() {
	// Commenting this for a while to prevent build error.

	// r.mu.Lock()
	// nextPrimaryID = r.nextPrimary(r.primaryID)
	// newViewNum := r.viewNum
	// r.mu.Unlock()

	// args := DoViewChangeArgs{}
	// var reply DoViewChangeReply

	// r.dlog("sending <DO-VIEW-CHANGE> to the next primary %d: %+v", nextPrimaryID, args)
	// if err := r.server.Call(nextPrimaryID, "Replica.DoViewChange", args, &reply); err == nil {

	// }
}

func (r *Replica) initiateViewChange() {
	// r.dlog("Timed out, initiate viewchange")
	r.status = ViewChange
	r.viewNum += 1
	savedCurrentViewNum := r.viewNum
	r.viewChangeResetEvent = time.Now()
	r.dlog("initiates VIEW CHANGE; view=%d; log=<ADDED LATER>", savedCurrentViewNum)

	// for peerID := range r.configuration {
	// 	go func(peerID int) {
	// 		args := StartViewChangeArgs{
	// 			viewNum:   savedCurrentViewNum,
	// 			replicaID: r.ID,
	// 		}
	// 		var reply StartViewChangeReply

	// 		r.dlog("sending <START-VIEW-CHANGE> to %d: %+v", peerID, args)
	// 		if err := r.server.Call(peerID, "Replica.StartViewChange", args, &reply); err == nil {
	// 			r.mu.Lock()
	// 			defer r.mu.Unlock()
	// 			r.dlog("received <START-VIEW-CHANGE> reply %+v", reply)
	// 			// reply.isReplied = true
	// 			return
	// 		}
	// 	}(peerID)
	// }

	// Run another ViewChangeTimer in case this ViewChange is failed.
	go r.runViewChangeTimer()
}

type DoViewChangeArgs struct{}

type DoViewChangeReply struct{}

func (r *Replica) DoViewChange(args DoViewChangeArgs, reply *DoViewChangeReply) error {
	return nil
}

type StartViewChangeArgs struct {
	viewNum   int
	replicaID int
}

type StartViewChangeReply struct {
	isReplied bool
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
	if args.viewNum > r.viewNum {
		// Set status to `view-change`, set `view-num` to the message's `view-num`
		// and reply with <START-VIEW-CHANGE> to all replicas.
		reply.isReplied = true
		r.status = ViewChange
		r.viewNum = args.viewNum
		// savedCurrentViewNum := r.viewNum
		r.viewChangeResetEvent = time.Now()

		// TODO
		// for peerID := range r.configuration {
		// 	go func(peerID int) {
		// 		args := StartViewChangeArgs{
		// 			viewNum:   savedCurrentViewNum,
		// 			replicaID: r.ID,
		// 		}
		// 		var reply StartViewChangeReply

		// 		r.dlog("received <START-VIEW-CHANGE>, will also send it to %d: %+v", peerID, args)
		// 		if err := r.server.Call(peerID, "Replica.StartViewChange", args, &reply); err == nil {
		// 			// reply.isReplied = true
		// 			return
		// 		}
		// 	}(peerID)
		// }
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

func (r *Replica) nextPrimary(primaryID int) int {
	nextPrimaryID := r.primaryID + 1
	if nextPrimaryID == len(r.configuration)+1 {
		nextPrimaryID = 0
	}

	return nextPrimaryID
}
