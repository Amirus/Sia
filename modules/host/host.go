package host

import (
	"errors"
	"os"
	"sync"

	"github.com/NebulousLabs/Sia/consensus"
	"github.com/NebulousLabs/Sia/modules"
)

const (
	// StorageProofReorgDepth states how many blocks to wait before submitting
	// a storage proof. This reduces the chance of needing to resubmit because
	// of a reorg.
	StorageProofReorgDepth = 20
	maxContractLen         = 1 << 16 // The maximum allowed size of a file contract coming in over the wire. This does not include the file.
)

// A contractObligation tracks a file contract that the host is obligated to
// fulfill.
type contractObligation struct {
	ID           consensus.FileContractID
	FileContract consensus.FileContract
	Path         string // Where on disk the file is stored.
}

// A Host contains all the fields necessary for storing files for clients and
// performing the storage proofs on the received files.
type Host struct {
	state       *consensus.State
	tpool       modules.TransactionPool
	wallet      modules.Wallet
	latestBlock consensus.BlockID

	saveDir        string
	spaceRemaining int64
	fileCounter    int

	obligationsByID     map[consensus.FileContractID]contractObligation
	obligationsByHeight map[consensus.BlockHeight][]contractObligation

	modules.HostSettings

	mu sync.RWMutex
}

// New returns an initialized Host.
func New(state *consensus.State, tpool modules.TransactionPool, wallet modules.Wallet, saveDir string) (h *Host, err error) {
	if state == nil {
		err = errors.New("host cannot use a nil state")
		return
	}
	if tpool == nil {
		err = errors.New("host cannot use a nil tpool")
		return
	}
	if wallet == nil {
		err = errors.New("host cannot use a nil wallet")
		return
	}

	addr, _, err := wallet.CoinAddress()
	if err != nil {
		return
	}
	h = &Host{
		state:  state,
		tpool:  tpool,
		wallet: wallet,

		// default host settings
		HostSettings: modules.HostSettings{
			TotalStorage: 2e9,                          // 2 GB
			MaxFilesize:  300e6,                        // 300 MB
			MaxDuration:  5e3,                          // Just over a month.
			WindowSize:   288,                          // 48 hours.
			Price:        consensus.NewCurrency64(1e9), // 10^9
			Collateral:   consensus.NewCurrency64(0),
			UnlockHash:   addr,
		},

		saveDir:        saveDir,
		spaceRemaining: 2e9,

		obligationsByID:     make(map[consensus.FileContractID]contractObligation),
		obligationsByHeight: make(map[consensus.BlockHeight][]contractObligation),
	}
	block, exists := state.BlockAtHeight(0)
	if !exists {
		err = errors.New("state doesn't have a genesis block")
		return
	}
	h.latestBlock = block.ID()

	err = os.MkdirAll(saveDir, 0700)
	if err != nil {
		return
	}
	h.load()

	consensusChan := state.SubscribeToConsensusChanges()
	go h.threadedConsensusListen(consensusChan)

	return
}

// SetConfig updates the host's internal HostSettings object. To modify
// a specific field, use a combination of Info and SetConfig
func (h *Host) SetSettings(settings modules.HostSettings) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.HostSettings = settings
	h.save()
}

// Settings is an RPC used to request the settings of a host.
func (h *Host) Settings(conn modules.NetConn) error {
	h.mu.RLock()
	hs := h.HostSettings
	h.mu.RUnlock()
	return conn.WriteObject(hs)
}

func (h *Host) Info() modules.HostInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	info := modules.HostInfo{
		HostSettings: h.HostSettings,

		StorageRemaining: h.spaceRemaining,
		NumContracts:     len(h.obligationsByID),
	}
	return info
}
