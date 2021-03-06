package consensus

import (
	"errors"
	"math/big"
	"sort"

	"github.com/NebulousLabs/Sia/crypto"
)

// StateInfo contains basic information about the State.
type StateInfo struct {
	CurrentBlock BlockID
	Height       BlockHeight
	Target       Target
}

// blockAtHeight returns the block on the current path with the given height.
func (s *State) blockAtHeight(height BlockHeight) (b Block, exists bool) {
	exists = height <= s.height()
	if exists {
		b = s.blockMap[s.currentPath[height]].block
	}
	return
}

// currentBlockID returns the ID of the current block.
func (s *State) currentBlockID() BlockID {
	return s.currentPath[s.height()]
}

// currentBlockNode returns the blockNode of the current block.
func (s *State) currentBlockNode() *blockNode {
	return s.blockMap[s.currentBlockID()]
}

// currentBlockWeight returns the weight of the current block.
func (s *State) currentBlockWeight() *big.Rat {
	return s.currentBlockNode().target.Inverse()
}

// height returns the current height of the state.
func (s *State) height() BlockHeight {
	return BlockHeight(len(s.currentPath) - 1)
}

// output returns the unspent SiacoinOutput associated with the given ID. If
// the output is not in the UTXO set, 'exists' will be false.
func (s *State) output(id SiacoinOutputID) (sco SiacoinOutput, exists bool) {
	sco, exists = s.siacoinOutputs[id]
	return
}

// sortedUscoSet returns all of the unspent siacoin outputs sorted
// according to the numerical value of their id.
func (s *State) sortedUscoSet() []SiacoinOutput {
	// Get all of the outputs in string form and sort the strings.
	unspentOutputs := make(crypto.HashSlice, len(s.siacoinOutputs))
	for outputID := range s.siacoinOutputs {
		unspentOutputs = append(unspentOutputs, crypto.Hash(outputID))
	}
	sort.Sort(unspentOutputs)

	// Get the outputs in order according to their sorted form.
	sortedOutputs := make([]SiacoinOutput, len(unspentOutputs))
	for i, outputID := range unspentOutputs {
		output, _ := s.output(SiacoinOutputID(outputID))
		sortedOutputs[i] = output
	}
	return sortedOutputs
}

// Sorted UsfoSet returns all of the unspent siafund outputs sorted according
// to the numerical value of their id.
func (s *State) sortedUsfoSet() []SiafundOutput {
	// Get all of the outputs in string form and sort the strings.
	outputIDs := make(crypto.HashSlice, len(s.siafundOutputs))
	for outputID := range s.siafundOutputs {
		outputIDs = append(outputIDs, crypto.Hash(outputID))
	}
	sort.Sort(outputIDs)

	// Get the outputs in order according to their sorted string form.
	sortedOutputs := make([]SiafundOutput, len(outputIDs))
	for i, outputID := range outputIDs {
		// Sanity check - the output should exist.
		output, exists := s.siafundOutputs[SiafundOutputID(outputID)]
		if DEBUG {
			if !exists {
				panic("output doesn't exist")
			}
		}

		sortedOutputs[i] = output
	}
	return sortedOutputs
}

// BlockAtHeight returns the block on the current path with the given height.
func (s *State) BlockAtHeight(height BlockHeight) (b Block, exists bool) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)
	return s.blockAtHeight(height)
}

// Block returns the block associated with the given id.
func (s *State) Block(id BlockID) (b Block, exists bool) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)

	node, exists := s.blockMap[id]
	if !exists {
		return
	}
	b = node.block
	return
}

// BlockRange returns a slice of the blocks that fall within the given range
// [start, stop].
func (s *State) BlockRange(start, stop BlockHeight) ([]Block, error) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)

	if start > stop || stop > s.height() {
		return nil, errors.New("invalid range")
	}

	blocks := make([]Block, (stop-start)+1)
	for i, id := range s.currentPath[start : stop+1] {
		node, exists := s.blockMap[id]
		if !exists {
			if DEBUG {
				panic("blockMap is missing a block whose ID is in the currentPath")
			}
			return nil, errors.New("State is inconsistent")
		}
		blocks[i] = node.block
	}
	return blocks, nil
}

// BlockOutputDiffs returns the SiacoinOutputDiffs for a given block.
func (s *State) BlockOutputDiffs(id BlockID) (scods []SiacoinOutputDiff, err error) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)

	node, exists := s.blockMap[id]
	if !exists {
		err = errors.New("requested an unknown block")
		return
	}
	if !node.diffsGenerated {
		err = errors.New("diffs have not been generated for the requested block")
		return
	}
	scods = node.siacoinOutputDiffs
	return
}

// BlocksSince returns a set of output diffs representing how the state
// has changed since block 'id'. OutputDiffsSince will flip the `new` value for
// diffs that got reversed.
func (s *State) BlocksSince(id BlockID) (removedBlocks, addedBlocks []BlockID, err error) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)

	node, exists := s.blockMap[id]
	if !exists {
		err = errors.New("block is unknown")
		return
	}

	// Get all the IDs from the blockchain to the current path.
	path := s.backtrackToCurrentPath(node)
	for i := len(path) - 1; i > 0; i-- {
		removedBlocks = append(removedBlocks, path[i].block.ID())
	}

	// Get all the IDs going forward from the common parent.
	addedBlocks = s.currentPath[path[0].height+1:]
	return
}

// FileContract returns the file contract associated with the 'id'. If the
// contract does not exist, exists will be false.
func (s *State) FileContract(id FileContractID) (fc FileContract, exists bool) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)

	fc, exists = s.fileContracts[id]
	return
}

// CurrentBlock returns the highest block on the tallest fork.
func (s *State) CurrentBlock() Block {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)
	return s.currentBlockNode().block
}

func (s *State) ChildTarget(bid BlockID) (target Target, exists bool) {
	id := s.mu.RLock()
	defer s.mu.RUnlock(id)
	bn, exists := s.blockMap[bid]
	if !exists {
		return
	}
	target = bn.target
	return
}

// CurrentTarget returns the target of the next block that needs to be submitted
// to the state.
func (s *State) CurrentTarget() Target {
	id := s.mu.RLock()
	defer s.mu.RUnlock(id)
	return s.currentBlockNode().target
}

func (s *State) EarliestTimestamp() Timestamp {
	id := s.mu.RLock()
	defer s.mu.RUnlock(id)
	return s.currentBlockNode().earliestChildTimestamp()
}

// EarliestTimestamp returns the earliest timestamp that the next block can
// have in order for it to be considered valid.
func (s *State) EarliestChildTimestamp(bid BlockID) (timestamp Timestamp, exists bool) {
	id := s.mu.RLock()
	defer s.mu.RUnlock(id)
	bn, exists := s.blockMap[bid]
	if !exists {
		return
	}
	timestamp = bn.earliestChildTimestamp()
	return
}

// Height returns the height of the current blockchain (the longest fork).
func (s *State) Height() BlockHeight {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)
	return s.height()
}

// HeightOfBlock returns the height of the block with the given ID.
func (s *State) HeightOfBlock(bid BlockID) (height BlockHeight, exists bool) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)

	bn, exists := s.blockMap[bid]
	if !exists {
		return
	}
	height = bn.height
	return
}

// SiacoinOutput returns the siacoin output associated with the given ID.
func (s *State) SiacoinOutput(id SiacoinOutputID) (output SiacoinOutput, exists bool) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)
	return s.output(id)
}

// SiafundOutput returns the siafund output associated with the given ID.
func (s *State) SiafundOutput(id SiafundOutputID) (output SiafundOutput, exists bool) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)
	output, exists = s.siafundOutputs[id]
	return
}

// SortedUtxoSet returns all of the unspent transaction outputs sorted
// according to the numerical value of their id.
func (s *State) SortedUtxoSet() []SiacoinOutput {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)
	return s.sortedUscoSet()
}

// StorageProofSegment returns the segment to be used in the storage proof for
// a given file contract.
func (s *State) StorageProofSegment(fcid FileContractID) (index uint64, err error) {
	counter := s.mu.RLock()
	defer s.mu.RUnlock(counter)
	return s.storageProofSegment(fcid)
}
