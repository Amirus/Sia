package transactionpool

import (
	"errors"

	"github.com/NebulousLabs/Sia/consensus"
)

// valid.go checks that transactions are valid. The majority of the checking is
// done by the consensus package, using the call StandaloneValid. To minimize
// the potential for validation to break with the state, the only validation
// done by the transaction pool involves the unconfirmed consensus set and the
// IsStandard rules. It is imperative that if a transaction would be invalid
// according to the consensus package (after the dependency transactions have
// been confirmed) that it is also invalid according to the transaction pool.

var (
	ErrBadUnlockConditions      = errors.New("siacoin unlock conditions do not meet required unlock hash")
	ErrSiacoinOverspend         = errors.New("transaction has more siacoin outputs than inputs")
	ErrUnrecognizedSiacoinInput = errors.New("unrecognized siacoin input in transaction")
)

// validUnconfirmedSiacoins checks that all siacoin inputs and outputs are
// valid in the context of the unconfirmed consensus set.
func (tp *TransactionPool) validUnconfirmedSiacoins(t consensus.Transaction) (err error) {
	var inputSum consensus.Currency
	for _, sci := range t.SiacoinInputs {
		// All inputs must have corresponding outputs in the unconfirmed set.
		sco, exists := tp.siacoinOutputs[sci.ParentID]
		if !exists {
			return ErrUnrecognizedSiacoinInput
		}

		// The unlock conditions provided must match the unlock hash of the
		// corresponding output.
		if sci.UnlockConditions.UnlockHash() != sco.UnlockHash {
			return ErrBadUnlockConditions
		}

		inputSum = inputSum.Add(sco.Value)
	}

	// The sum of all inputs must equal the sum of all outputs.
	if inputSum.Cmp(t.SiacoinOutputSum()) != 0 {
		return ErrSiacoinOverspend
	}
	return
}

// validUnconfirmedStorageProofs checks that a storage proof is valid in the
// context of the unconfirmed consensus set.
func (tp *TransactionPool) validUnconfirmedStorageProofs(t consensus.Transaction) (err error) {
	// Check that the corresponding file contract is in the unconfirmed set.
	for _, sp := range t.StorageProofs {
		_, exists := tp.consensusSet.FileContract(sp.ParentID)
		if !exists {
			return errors.New("storage proof submitted for file contract not in confirmed set.")
		}
	}

	// Check that the storage proof is valid using the consensus set.
	err = tp.consensusSet.ValidStorageProofs(t)
	if err != nil {
		return
	}
	return
}

// validUnconfirmedFileContractTerminations checks that all file contract
// terminations are valid within the context of the unconfirmed consensus set.
func (tp *TransactionPool) validUnconfirmedFileContractTerminations(t consensus.Transaction) (err error) {
	for _, fct := range t.FileContractTerminations {
		// Check for the corresponding file contract in the unconfirmed set.
		fc, exists := tp.fileContracts[fct.ParentID]
		if !exists {
			return errors.New("termination given for unrecognized file contract")
		}

		// Check that the termination conditions match the termination hash of
		// the corresponding file contract.
		if fct.TerminationConditions.UnlockHash() != fc.TerminationHash {
			return errors.New("termination conditions do not meet required termination hash")
		}

		// Check that the termination was submitted before the storage proof
		// window opened.
		if fc.Start < tp.consensusSetHeight {
			return errors.New("termination submitted too late")
		}

		// Check that the payouts in the termination add up to the payout of
		// the contract.
		var payoutSum consensus.Currency
		for _, payout := range fct.Payouts {
			payoutSum = payoutSum.Add(payout.Value)
		}
		if payoutSum.Cmp(fc.Payout) != 0 {
			return errors.New("contract termination has incorrect payouts")
		}
	}
	return
}

// validUnconfirmedSiafunds checks that all siafund inputs and outputs are
// valid within the context of the unconfirmed consensus set.
func (tp *TransactionPool) validUnconfirmedSiafunds(t consensus.Transaction) (err error) {
	var inputSum consensus.Currency
	for _, sfi := range t.SiafundInputs {
		// Check that the corresponding siafund output being spent exists.
		sfo, exists := tp.siafundOutputs[sfi.ParentID]
		if !exists {
			return errors.New("transaction spends unrecognized siafund output")
		}

		// Check that the unlock conditions match the unlock hash of the
		// corresponding output.
		if sfi.UnlockConditions.UnlockHash() != sfo.UnlockHash {
			return errors.New("transaction contains invalid unlock conditions (hash mismatch)")
		}

		// Add this input's value to the inputSum.
		inputSum = inputSum.Add(sfo.Value)
	}

	// Check that the value of the outputs equal the value of the inputs.
	var outputSum consensus.Currency
	for _, sfo := range t.SiafundOutputs {
		outputSum = outputSum.Add(sfo.Value)
	}
	if outputSum.Cmp(inputSum) != 0 {
		return errors.New("siafund inputs do not equal siafund outputs")
	}

	return
}

// validUnconfirmedTransaction checks that the transaction would be valid in a
// block that contained all of the other unconfirmed transactions.
func (tp *TransactionPool) validUnconfirmedTransaction(t consensus.Transaction) (err error) {
	// Check that the transaction follows 'Standard.md' guidelines.
	err = tp.IsStandardTransaction(t)
	if err != nil {
		return
	}

	// Check that the transaction follows general rules - this check looks at
	// rules for transactions contianing storage proofs, the rules for file
	// contracts, and the rules for signatures.
	err = t.StandaloneValid(tp.consensusSetHeight)
	if err != nil {
		return
	}

	// Check the validity of the componenets in the context of the confirmed
	// and unconfirmed set.
	err = tp.validUnconfirmedSiacoins(t)
	if err != nil {
		return
	}
	err = tp.validUnconfirmedStorageProofs(t)
	if err != nil {
		return
	}
	// File contracts don't need to be checked as all potential problems are
	// checked by a combination of StandaloneValid and
	// ValidUnconfirmedSiacoins.
	err = tp.validUnconfirmedFileContractTerminations(t)
	if err != nil {
		return
	}
	err = tp.validUnconfirmedSiafunds(t)
	if err != nil {
		return
	}

	return
}
