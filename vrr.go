package vrr

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type CommitEntry struct {
	Command interface{}

	Index int

	View int
}

type ReplicaStatus int

const (
	Normal ReplicaStatus = iota
	Recovery
	ViewChange
	Transitioning
	Dead
	DoViewChange
	StartView
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
	case DoViewChange:
		return "DoViewChange"
	case StartView:
		return "StartView"
	default:
		panic("unreachable")
	}
}

type opLogEntry struct {
	opID      int
	operation interface{}
}

type Replica struct {
	mu sync.Mutex

	ID int

	server *Server

	commitChan         chan<- CommitEntry
	newCommitReadyChan chan struct{}

	oldViewNum int
	viewNum    int
	commitNum  int
	opNum      int
	opLog      []opLogEntry
	primaryID  int

	// These are used for saving data when the replica is the next designated primary
	// and are sorting out data from other backup replicas.
	doViewChangeCount int
	tempViewNum       int
	tempOpLog         []opLogEntry
	tempOpNum         int
	tempCommitNum     int

	status        ReplicaStatus
	configuration map[int]string

	// clientTable map is owned by every Replica and is a map
	// of the clientID to its request number, request operation, and response.
	clientTable map[int]clientTableEntry

	viewChangeResetEvent time.Time
}

type clientRequest struct {
	clientID int
	reqNum   int
	reqOp    interface{}
}

type clientTableEntry struct {
	reqNum int
	reqOp  interface{}
	resp   interface{}
}

func NewReplica(ID int, configuration map[int]string, server *Server, ready <-chan interface{}, commitChan chan<- CommitEntry) *Replica {
	replica := new(Replica)
	replica.ID = ID
	replica.configuration = configuration
	replica.server = server
	replica.commitChan = commitChan
	replica.newCommitReadyChan = make(chan struct{}, 16)
	replica.oldViewNum = -1
	replica.doViewChangeCount = 0
	replica.clientTable = make(map[int]clientTableEntry)

	replica.status = Normal

	go func() {
		<-ready
		replica.mu.Lock()
		replica.viewChangeResetEvent = time.Now()
		replica.mu.Unlock()
		replica.runViewChangeTimer()
	}()

	// go replica.commitChanSender()

	return replica
}

func (r *Replica) Report() (int, int, bool, ReplicaStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ID, r.viewNum, r.ID == r.primaryID, r.status
}

func (r *Replica) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = Dead
	r.dlog("becomes Dead")
	close(r.newCommitReadyChan)
}

func (r *Replica) Submit(req clientRequest) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.dlog("Submit received by %v: %v", r.status, req.reqOp)
	if r.ID != r.primaryID {
		r.dlog("is not a primary, dropping the request")
		return false
	}

	if r.status != Normal {
		r.dlog("is a primary but not in a Normal status, dropping the request")
		return false
	}

	r.opLog = append(r.opLog, opLogEntry{opID: len(r.opLog), operation: req.reqOp})
	r.opNum++
	ctEntry := clientTableEntry{
		reqNum: req.reqNum,
		reqOp:  req.reqOp,
	}
	r.clientTable[req.clientID] = ctEntry
	r.dlog("... log=%v", r.opLog)

	return true
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

		// Replica is the primary
		if r.status == Normal && r.primaryID == r.ID {
			// TODO
			// Implement the kind of sendLeaderHeartbeat
			r.dlog("as the Primary is sending <PREPARE> or <COMMIT> as the heartbeat messages; viewNum=%v; opNum=%v; commitNum=%v", r.viewNum, r.opNum, r.commitNum)
			r.primarySendHeartBeats()
			r.mu.Unlock()
			return
		}

		if r.status == ViewChange {
			r.dlog("status become View-Change, blast <START-VIEW-CHANGE> to all replicas")
			r.mu.Unlock()
			r.blastStartViewChange()
			return
		}

		if r.status == DoViewChange {
			r.sendDoViewChange()
			r.mu.Unlock()
			return
		}

		if r.status == StartView {
			r.dlog("status become Start-View as new designated primary, blast <START-VIEW> to all replicas for updated state.")
			r.mu.Unlock()
			r.blastStartView()
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

func (r *Replica) primarySendHeartBeats() {
	go func() {

	}()
}

func (r *Replica) blastStartViewChange() {
	savedCurrentViewNum := r.viewNum
	var repliesReceived int32 = 1
	var sendStartViewChangeAlready bool = false

	for peerID := range r.configuration {
		args := StartViewChangeArgs{
			ViewNum:   savedCurrentViewNum,
			ReplicaID: r.ID,
		}
		go func(peerID int) {
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
						r.initiateDoViewChange()
						sendStartViewChangeAlready = true
						return
					}
				}
			}
		}(peerID)
	}
}

func (r *Replica) initiateStartView() {
	r.status = StartView
	savedCurrentViewNum := r.viewNum
	r.viewChangeResetEvent = time.Now()
	r.dlog("initiates START VIEW; view=%d", savedCurrentViewNum)

	go r.runViewChangeTimer()
}

func (r *Replica) initiateDoViewChange() {
	r.status = DoViewChange
	savedCurrentViewNum := r.viewNum
	r.viewChangeResetEvent = time.Now()
	r.dlog("initiates DO VIEW CHANGE; view=%d", savedCurrentViewNum)

	go r.runViewChangeTimer()
}

func (r *Replica) sendDoViewChange() {
	nextPrimaryID := nextPrimary(r.primaryID, r.configuration)

	if nextPrimaryID == r.ID {
		r.doViewChangeCount++
		return
	}

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
	r.doViewChangeCount = 0
	r.viewNum += 1
	savedCurrentViewNum := r.viewNum
	r.viewChangeResetEvent = time.Now()
	r.dlog("initiates VIEW CHANGE; view=%d; log=<ADDED LATER>", savedCurrentViewNum)

	go r.runViewChangeTimer()
}

func (r *Replica) blastStartView() {
	r.mu.Lock()
	savedViewNum := r.viewNum
	savedOpLog := r.opLog
	savedOpNum := r.opNum
	savedPrimaryID := r.ID
	r.mu.Unlock()

	for peerID := range r.configuration {
		args := StartViewArgs{
			ViewNum:   savedViewNum,
			OpLog:     savedOpLog,
			OpNum:     savedOpNum,
			PrimaryID: savedPrimaryID,
		}
		go func(peerID int) {
			var reply StartViewReply

			r.dlog("sending <START-VIEW> to %d: %+v", peerID, args)
			err := r.server.Call(peerID, "Replica.StartView", args, &reply)
			if err != nil {
				log.Println(err)
			}
			if err == nil {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.dlog("received <START-VIEW> reply +%v", reply)
				return
			}
		}(peerID)
	}
}

type PrepareArgs struct {
	ViewNum       int
	OpNum         int
	CommitNum     int
	ClientMessage clientRequest
}

type PrepareOKReply struct {
	ViewNum   int
	OpNum     int
	ReplicaID int
	Status    ReplicaStatus
}

func (r *Replica) Prepare(args PrepareArgs, reply *PrepareOKReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status == Dead {
		return nil
	}
	r.dlog("Prepare: %+v [currentView=%d]", args, r.viewNum)

	// TODO
	// This Replica is behind others, changing status to Recovery and
	// initiate state transfer from the new primary.
	if r.viewNum < args.ViewNum {
		r.status = Recovery
		r.dlog("is behind PREPARE's viewNum, changing status to Recovery and initiate state transfer from Primary")

		// TODO
		// Initiate a state transfer from the Primary.
		// NOTE: Will probably need to run timer here.
	}

	if r.viewNum == args.ViewNum && r.opNum == args.OpNum-1 {
		// Not only the viewNum should be the same,
		// but also the opNum should be strictly consecutive.
		// If not, replica drops the message and initiates recovery with state transfer
		if r.opNum != args.OpNum-1 {
			r.status = Recovery
			r.dlog("viewNum is the same but different opNum with PREPARE's, changing status to Recovery and initiate state transfer from Primary")

			// TODO
			// Initiate recovery with state transfer.
			// Note: Will probably need to run timer here.

			return nil
		}

		r.opNum++
		r.opLog = append(r.opLog, opLogEntry{opID: len(r.opLog), operation: args.ClientMessage.reqOp})
		ctEntry := clientTableEntry{
			reqNum: args.ClientMessage.reqNum,
			reqOp:  args.ClientMessage.reqOp,
		}
		r.clientTable[args.ClientMessage.clientID] = ctEntry

		reply.ReplicaID = r.ID
		reply.Status = r.status
		reply.ViewNum = r.viewNum
		reply.OpNum = r.opNum

		r.dlog("... PREPARE-OK replied: %+v", reply)
	}

	// This also returns nil when this Replica's viewNum is greater (>)
	// than the incoming argument's viewNum (r.viewNum > args.ViewNum)
	// which means this replica drops the incoming message.
	if r.viewNum > args.ViewNum {
		r.dlog("viewNum is bigger than PREPARE's, drops message")
	}

	return nil
}

type CommitArgs struct{}

type CommitReply struct{}

func (r *Replica) Commit(args CommitArgs, reply *CommitReply) error {

	return nil
}

type StartViewArgs struct {
	ViewNum   int
	OpLog     []opLogEntry
	OpNum     int
	PrimaryID int
}

type StartViewReply struct {
	IsReplied bool
	ReplicaID int
}

func (r *Replica) StartView(args StartViewArgs, reply *StartViewReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status == Dead {
		return nil
	}
	r.dlog("StartView: %+v [currentView=%d]", args, r.viewNum)

	reply.IsReplied = true
	reply.ReplicaID = r.ID
	// var oldOpNum = r.opNum

	r.opLog = args.OpLog
	r.opNum = args.OpNum
	r.viewNum = args.ViewNum
	r.primaryID = args.PrimaryID

	r.status = Normal
	// TODO
	// 1. Replica executes all operation from the old commitNum to the new commitNum.
	// 2. Send <PREPARE-OK> for all operations in opLog which have not been commited yet.

	// go r.runViewChangeTimer()

	return nil
}

type DoViewChangeArgs struct {
	ViewNum    int
	OldViewNum int
	CommitNum  int
	OpNum      int
	OpLog      []opLogEntry
}

type DoViewChangeReply struct {
	IsReplied bool
	ReplicaID int
}

func (r *Replica) DoViewChange(args DoViewChangeArgs, reply *DoViewChangeReply) error {
	r.mu.Lock()

	if r.status == Dead {
		return nil
	}
	r.dlog("DoViewChange: %+v [currentView=%d]", args, r.viewNum)

	if args.ViewNum == r.viewNum {
		r.doViewChangeCount++
		r.dlog("DoViewChange messages received: %d", r.doViewChangeCount)

		if args.OldViewNum >= r.oldViewNum {
			if args.OpNum > r.opNum {
				r.tempViewNum = args.ViewNum
				r.tempOpNum = len(args.OpLog)
				r.tempOpLog = args.OpLog
			}
		}

		if args.CommitNum >= r.commitNum {
			r.tempCommitNum = args.CommitNum
		}
	}

	if r.doViewChangeCount > (len(r.configuration)/2)+1 && r.status != StartView {
		// WORKING
		// Comparing messages to other replicas' data and taking the most updated/recent state.
		// Primary is back to normal and informs other replicas of the completion of the View-Change
		r.viewNum = r.tempViewNum
		r.opNum = r.tempOpNum
		r.opLog = r.tempOpLog

		// TODO
		// Execute all commited operations in the operation log between
		// the old commitNum and the new commitNum (r.tempCommitNum)

		r.commitNum = r.tempCommitNum
		r.status = Normal
		r.primaryID = r.ID
		r.dlog("as Primary is back to Normal; viewNum = %v; opNum = %v; commitNum = %v; ", r.viewNum, r.opNum, r.commitNum)
		r.initiateStartView()
		r.mu.Unlock()

		return nil
	}

	r.mu.Unlock()
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
