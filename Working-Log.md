LAST PART:

- DoViewChange, read Acolyer blog
    - oldViewNum

=== FOR BLOG ===
- belum terlalu efektif karena berurutan banget itu dari startviewchange, doviewchange, sampe startview, masih lumayan tightly coupled
- ada banyak temp variable buat next designated primary
- PrepareOK is not necessarily a reply but a replica can send it when primary is starting a new view
- still mostly use mutex and not channel
- lebih banyak passing mesage dan perubahan state walaupun "ga ada leader election" dibanding raft
- sempet stuck lama karena mau ngirim doviewchange ke diri sendiri dan stuck karena ngelock pas mau rpc tapi ya masih ada lock pas di timer 
- kadang butuh baca paper RVV, ataupun bandingin implementasi eliben sama baca paper Raft

=== NOTES ===
- cm.ElectionResetEvent = time.Now() is called in :
    - NewConsensusModule
    - RequestVote RPC
    - AppendEntries RPC
    - startElection
    - becomeFollower
- cm.runElectionTimer() is called in:
    - startElection
    - becomeFollower

=== WORKING LOG ===

20/4/2020
[ ] add time.Now() at the appropriate places
[ ] 

21/4/2020
[ ] Implementing DoViewChange and StartView as Replica State
[ ] mungkin harus diimplement di runviewtimer atau ubah2 time.now

[ ] lagi ngetrace satu-satu, sekarang di blastStartViewChange

27/4/2020
[x] Merging DoViewChange to master

29/4/2020
(?) What's lastApplied for on the eliben/raft
[ ] Work on Log replication in normal mode

30/4/2020
[ ] Continuing Log Replication, to primary's quorum
[ ] Primary's heartbeat mechanism: PREPARE
[ ] Replica's applying operation:
    [ ] PREPARE
    [ ] COMMIT
        - time (?)

NIT
{->} Primary has not change its status to Normal after "elected" and initiating VIEW-CHANGE
{->} OldViewNum should also be changed when in Normal operation, not only in VIEW-CHANGE

1/5/2020
{v} Primary has not change its status to Normal after "elected" and initiating VIEW-CHANGE
{v} Updating all the primaryID at all Replicas when new Primary is chosen.
{ } OldViewNum should also be changed when in Normal operation, not only in VIEW-CHANGE

2/5/2020 - Saturday
- Design decision on sending <PREPARE>, we can:
    - send <PREPARE> immediately on the Submit method when there's new incoming request to the Primary or
    - in hearbeat method, check whether opNum equal commitNum and send <PREPARE> when it's unequal

    yet, the second option make it impossible for Primary to know the clientID the operation that has not been commited belongs to, so let's try the former option first while heartbeat can be used for <COMMIT> messages when there's no incoming request.

3/5/2020 - Sunday
- Do not forget to add time.Now() to reset the timer on all possible state change

=== TO DO ===
[ ] Implementing Replica's recovery
[ ] Protocol optimisations
[ ] Replica's ViewChange recovery after partition loss

[ ] Checking whether timer is already reseted on all possible state changes