package miner

import (
	"errors"
	"sync"

	"github.com/NebulousLabs/Sia/consensus"
	"github.com/NebulousLabs/Sia/modules"
)

type Miner struct {
	state   *consensus.State
	gateway modules.Gateway
	tpool   modules.TransactionPool
	wallet  modules.Wallet

	// Block variables - helps the miner construct the next block.
	parent            consensus.BlockID
	transactions      []consensus.Transaction
	target            consensus.Target
	earliestTimestamp consensus.Timestamp
	address           consensus.UnlockHash

	threads              int // how many threads the miner uses, shouldn't ever be 0.
	desiredThreads       int // 0 if not mining.
	runningThreads       int
	iterationsPerAttempt uint64

	subscribers []chan struct{}

	mu sync.RWMutex
}

// New returns a ready-to-go miner that is not mining.
func New(s *consensus.State, g modules.Gateway, tpool modules.TransactionPool, w modules.Wallet) (m *Miner, err error) {
	if s == nil {
		err = errors.New("miner cannot use a nil state")
		return
	}
	if g == nil {
		err = errors.New("miner cannot use a nil gateway")
		return
	}
	if tpool == nil {
		err = errors.New("miner cannot use a nil transaction pool")
		return
	}
	if w == nil {
		err = errors.New("miner cannot use a nil wallet")
		return
	}

	m = &Miner{
		state:   s,
		gateway: g,
		tpool:   tpool,
		wallet:  w,

		parent:            s.CurrentBlock().ID(),
		target:            s.CurrentTarget(),
		earliestTimestamp: s.EarliestTimestamp(),

		threads:              1,
		iterationsPerAttempt: 16 * 1024,
	}

	addr, _, err := m.wallet.CoinAddress()
	if err != nil {
		return
	}
	m.address = addr

	m.tpool.TransactionPoolSubscribe(m)
	return
}

// SetThreads establishes how many threads the miner will use when mining.
func (m *Miner) SetThreads(threads int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if threads == 0 {
		return errors.New("cannot have a miner with 0 threads.")
	}
	m.threads = threads

	return nil
}
