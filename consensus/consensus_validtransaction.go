package consensus

import (
	"errors"
	"math/big"

	"github.com/NebulousLabs/Sia/crypto"
)

// validSiacoins checks that the siacoin inputs and outputs are valid in the
// context of the current consensus set.
func (s *State) validSiacoins(t Transaction) (err error) {
	var inputSum Currency
	for _, sci := range t.SiacoinInputs {
		// Check that the input spends an existing output.
		sco, exists := s.siacoinOutputs[sci.ParentID]
		if !exists {
			return ErrMissingSiacoinOutput
		}

		// Check that the unlock conditions match the required unlock hash.
		if sci.UnlockConditions.UnlockHash() != sco.UnlockHash {
			return errors.New("siacoin unlock conditions do not meet required unlock hash")
		}

		inputSum = inputSum.Add(sco.Value)
	}
	if inputSum.Cmp(t.SiacoinOutputSum()) != 0 {
		return errors.New("inputs do not equal outputs for transaction")
	}
	return
}

// storageProofSegment returns the index of the segment that needs to be proven
// exists in a file contract.
func (s *State) storageProofSegment(fcid FileContractID) (index uint64, err error) {
	// Get the file contract associated with the input id.
	fc, exists := s.fileContracts[fcid]
	if !exists {
		err = errors.New("unrecognized file contract id")
		return
	}

	// Get the ID of the trigger block.
	triggerHeight := fc.Start - 1
	if triggerHeight > s.height() {
		err = errors.New("no block found at contract trigger block height")
		return
	}
	triggerID := s.currentPath[triggerHeight]

	// Get the index by appending the file contract ID to the trigger block and
	// taking the hash, then converting the hash to a numerical value and
	// modding it against the number of segments in the file. The result is a
	// random number in range [0, numSegments]. The probability is very
	// slightly weighted towards the beginning of the file, but because the
	// size difference between the number of segments and the random number
	// being modded, the difference is too small to make any practical
	// difference.
	seed := crypto.HashBytes(append(triggerID[:], fcid[:]...))
	numSegments := int64(crypto.CalculateSegments(fc.FileSize))
	seedInt := new(big.Int).SetBytes(seed[:])
	index = seedInt.Mod(seedInt, big.NewInt(numSegments)).Uint64()
	return
}

// validStorageProofs checks that the storage proofs are valid in the context
// of the consensus set.
func (s *State) validStorageProofs(t Transaction) error {
	for _, sp := range t.StorageProofs {
		fc, exists := s.fileContracts[sp.ParentID]
		if !exists {
			return errors.New("unrecognized file contract ID in storage proof")
		}

		// Check that the storage proof itself is valid.
		segmentIndex, err := s.storageProofSegment(sp.ParentID)
		if err != nil {
			return err
		}

		verified := crypto.VerifySegment(
			sp.Segment,
			sp.HashSet,
			crypto.CalculateSegments(fc.FileSize),
			segmentIndex,
			fc.FileMerkleRoot,
		)
		if !verified {
			return errors.New("provided storage proof is invalid")
		}
	}

	return nil
}

// validFileContractTerminations checks that each file contract termination is
// valid in the context of the current consensus set.
func (s *State) validFileContractTerminations(t Transaction) (err error) {
	for _, fct := range t.FileContractTerminations {
		// Check that the FileContractTermination terminates an existing
		// FileContract.
		fc, exists := s.fileContracts[fct.ParentID]
		if !exists {
			return ErrMissingFileContract
		}

		// Check that the height is less than fc.Start - terminations are not
		// allowed to be submitted once the storage proof window has opened.
		// This reduces complexity for unconfirmed transactions.
		if fc.Start < s.height() {
			return errors.New("contract termination submitted too late")
		}

		// Check that the unlock conditions match the unlock hash.
		if fct.TerminationConditions.UnlockHash() != fc.TerminationHash {
			return errors.New("termination conditions don't match required termination hash")
		}

		// Check that the payouts in the termination add up to the payout of the
		// contract.
		var payoutSum Currency
		for _, payout := range fct.Payouts {
			payoutSum = payoutSum.Add(payout.Value)
		}
		if payoutSum.Cmp(fc.Payout) != 0 {
			return errors.New("contract termination has incorrect payouts")
		}
	}

	return
}

// validSiafunds checks that the siafund portions of the transaction are valid
// in the context of the consensus set.
func (s *State) validSiafunds(t Transaction) (err error) {
	// Compare the number of input siafunds to the output siafunds.
	var siafundInputSum Currency
	var siafundOutputSum Currency
	for _, sfi := range t.SiafundInputs {
		sfo, exists := s.siafundOutputs[sfi.ParentID]
		if !exists {
			return ErrMissingSiafundOutput
		}

		// Check the unlock conditions match the unlock hash.
		if sfi.UnlockConditions.UnlockHash() != sfo.UnlockHash {
			return errors.New("unlock conditions don't match required unlock hash")
		}

		siafundInputSum = siafundInputSum.Add(sfo.Value)
	}
	for _, sfo := range t.SiafundOutputs {
		siafundOutputSum = siafundOutputSum.Add(sfo.Value)
	}
	if siafundOutputSum.Cmp(siafundInputSum) != 0 {
		return errors.New("siafund inputs do not equal siafund outpus within transaction")
	}
	return
}

// validTransaction checks that all fields are valid within the current
// consensus state. If not an error is returned.
func (s *State) validTransaction(t Transaction) (err error) {
	// StandaloneValid will check things like signatures and properties that
	// should be inherent to the transaction. (storage proof rules, etc.)
	err = t.StandaloneValid(s.height())
	if err != nil {
		return
	}

	// Check that each portion of the transaction is legal given the current
	// consensus set.
	err = s.validSiacoins(t)
	if err != nil {
		return
	}
	err = s.validFileContractTerminations(t)
	if err != nil {
		return
	}
	err = s.validStorageProofs(t)
	if err != nil {
		return
	}
	err = s.validSiafunds(t)
	if err != nil {
		return
	}

	return
}

// ValidStorageProofs checks that the storage proofs are valid in the context
// of the consensus set.
func (s *State) ValidStorageProofs(t Transaction) (err error) {
	id := s.mu.RLock()
	defer s.mu.RUnlock(id)
	return s.validStorageProofs(t)
}