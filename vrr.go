package vrr

type Replica struct{}

func NewReplica() *Replica {
	return &Replica{}
}

type TestArgs struct{}

type TestReply struct{}

func (r *Replica) TestRPC(args TestArgs, reply *TestReply) error {
	return nil
}
