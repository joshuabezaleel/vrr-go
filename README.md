# Viewstamped Replication Revisited in Go

This is work in progress of the implementation of distributed consensus/log replication protocol of Viewstamped Replication Revisited [1] in Go, an algorithm that still mostly overlooked beside its alternative, Raft and Paxos. 

## Progress state
- View Change (equivalent to leader election in other algorithms) is working already.
- Log replication (submitting and commiting new client's request) is still in progress.

A sadly poorly made working log can be seen at the [Working-Log](Working-Log.md). 

## Acknowledgement
This project is highly inspired and a large part of the barebone of the code (server structure, rpc, etc.) not including the logic of the Viewstamped Replication Revisited algorithm is adapted from Eli Bendersky's [Go implementation of Raft](https://github.com/eliben/raft/) and its awesome crystal clear series of [blog posts](https://eli.thegreenplace.net/2020/implementing-raft-part-0-introduction/). Huge thank you to Eli. I encourage all people interested in Go and/or distributed consensus protocol to read it. 
Other blog posts that help me understand the algorithm are [Bruno Bonacci's](https://blog.brunobonacci.com/2018/07/15/viewstamped-replication-explained/) and [Adrian Colyer's](https://blog.acolyer.org/2015/03/06/viewstamped-replication-revisited/).

## References
1 [Liskov, B., and Cowling, J. Viewstamped replication revisited. Tech. Rep. MIT-CSAIL-TR-2012-021, MIT,
July 2012.](https://dspace.mit.edu/bitstream/handle/1721.1/71763/MIT-CSAIL-TR-2012-021.pdf?sequence=1)

## LICENSE
Project is licensed under the terms of MIT license.