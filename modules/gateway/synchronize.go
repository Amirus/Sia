package gateway

import (
	"time"

	"github.com/NebulousLabs/Sia/consensus"
	"github.com/NebulousLabs/Sia/crypto"
	"github.com/NebulousLabs/Sia/modules"
)

const (
	MaxCatchUpBlocks = 50
)

// threadedResynchronize continuously calls Synchronize on a random peer every
// few minutes. This helps prevent unintentional desychronization in the event
// that broadcasts start failing.
func (g *Gateway) threadedResynchronize() {
	for {
		peer, err := g.randomPeer()
		if err != nil {
			// fatal error
			return
		}
		go g.Synchronize(peer)
		time.Sleep(time.Minute * 2)
	}
}

// Synchronize synchronizes the local consensus set (i.e. the blockchain) with
// the network consensus set. The process is as follows: synchronize asks a
// peer for new blocks. The requester sends 32 block IDs, starting with the 12
// most recent and then progressing exponentially backwards to the genesis
// block. The receiver uses these blocks to find the most recent block seen by
// both peers. From this starting height, it transmits blocks sequentially.
// The requester then integrates these blocks into its consensus set. Multiple
// such transmissions may be required to fully synchronize.
//
// TODO: don't run two Synchronize threads at the same time
func (g *Gateway) Synchronize(peer modules.NetAddress) error {
	for {
		var newBlocks []consensus.Block
		newBlocks, moreAvailable, err := g.requestBlocks(peer)
		if err != nil {
			return err
		}
		for _, block := range newBlocks {
			acceptErr := g.state.AcceptBlock(block)
			if acceptErr != nil {
				// TODO: If the error is a FutureTimestampErr, need to wait before trying the
				// block again.
			}
		}

		// loop until there are no more blocks available
		if !moreAvailable {
			break
		}
	}
	return nil
}

// sendBlocks returns a sequential set of blocks based on the 32 input block
// IDs. The most recent known ID is used as the starting point, and up to
// 'MaxCatchUpBlocks' from that BlockHeight onwards are returned. It also
// sends a boolean indicating whether more blocks are available.
func (g *Gateway) sendBlocks(conn modules.NetConn) (err error) {
	// Read known blocks.
	var knownBlocks [32]consensus.BlockID
	err = conn.ReadObject(&knownBlocks, 32*crypto.HashSize)
	if err != nil {
		return
	}

	// Find the most recent block from knownBlocks that is in our current path.
	found := false
	var start consensus.BlockHeight
	for _, id := range knownBlocks {
		if height, exists := g.state.HeightOfBlock(id); exists {
			found = true
			start = height + 1 // start at child
			break
		}
	}
	// If we didn't find any matching blocks, or if we're already
	// synchronized, don't send any blocks. The genesis block should be
	// included in knownBlocks, so if no matching blocks are found, the caller
	// is probably on a different blockchain altogether.
	if !found || start > g.state.Height() {
		// Send 0 blocks.
		err = conn.WriteObject([]consensus.Block{})
		if err != nil {
			return
		}
		// Indicate that no more blocks are available.
		return conn.WriteObject(false)
	}

	// Determine range of blocks to send.
	stop := start + MaxCatchUpBlocks
	if stop > g.state.Height() {
		stop = g.state.Height()
	}
	blocks, err := g.state.BlockRange(start, stop)
	if err != nil {
		return
	}
	err = conn.WriteObject(blocks)
	if err != nil {
		return
	}

	// Indicate whether more blocks are available.
	more := g.state.Height() > stop
	return conn.WriteObject(more)
}

func (g *Gateway) requestBlocks(peer modules.NetAddress) (newBlocks []consensus.Block, moreAvailable bool, err error) {
	history := g.blockHistory()
	err = g.RPC(peer, "SendBlocks", func(conn modules.NetConn) error {
		err := conn.WriteObject(history)
		if err != nil {
			return err
		}
		err = conn.ReadObject(&newBlocks, MaxCatchUpBlocks*consensus.BlockSizeLimit)
		if err != nil {
			return err
		}
		return conn.ReadObject(&moreAvailable, 1)
	})
	return
}

// blockHistory returns up to 32 BlockIDs, starting with the 12 most recent
// BlockIDs and then doubling in step size until the genesis block is reached.
// The genesis block is always included. This array of BlockIDs is used to
// establish a shared commonality between peers during synchronization.
func (g *Gateway) blockHistory() (blockIDs [32]consensus.BlockID) {
	knownBlocks := make([]consensus.BlockID, 0, 32)
	step := consensus.BlockHeight(1)
	for height := g.state.Height(); ; height -= step {
		block, exists := g.state.BlockAtHeight(height)
		if !exists {
			// faulty state; log high-priority error
			return
		}
		knownBlocks = append(knownBlocks, block.ID())

		// after 12, start doubling
		if len(knownBlocks) >= 12 {
			step *= 2
		}

		// this check has to come before height -= step;
		// otherwise we might underflow
		if height <= step {
			break
		}
	}

	// always include the genesis block
	genesis, exists := g.state.BlockAtHeight(0)
	if !exists {
		// this should never happen
		return
	}
	knownBlocks = append(knownBlocks, genesis.ID())

	copy(blockIDs[:], knownBlocks)
	return
}
