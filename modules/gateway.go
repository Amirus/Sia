package modules

import (
	"io"
	"net"

	"github.com/NebulousLabs/Sia/consensus"
	"github.com/NebulousLabs/Sia/encoding"
)

const (
	GatewayDir = "gateway"
)

// TODO: Move this and it's functionality into the gateway package.
var (
	BootstrapPeers = []NetAddress{
		"23.239.14.98:9988",
	}
)

// A NetConn is a monitored network connection.
type NetConn interface {
	io.ReadWriteCloser

	// ReadWriters support reading and writing objects, which are encoded and
	// decoded via the encoding package.
	encoding.ReadWriter

	// Addr returns the NetAddress of the remote end of the connection.
	Addr() NetAddress
}

// RPCFunc is the type signature of functions that handle incoming RPCs.
type RPCFunc func(NetConn) error

// A NetAddress contains the information needed to contact a peer.
type NetAddress string

type GatewayInfo struct {
	Address NetAddress
	Peers   []NetAddress
}

// Host returns the NetAddress' IP.
func (na NetAddress) Host() string {
	host, _, _ := net.SplitHostPort(string(na))
	return host
}

// Port returns the NetAddress' port number.
func (na NetAddress) Port() string {
	_, port, _ := net.SplitHostPort(string(na))
	return port
}

// A Gateway facilitates the interactions between the local node and remote
// nodes (peers). It relays incoming blocks and transactions to local modules,
// and broadcasts outgoing blocks and transactions to peers. In a broad sense,
// it is responsible for ensuring that the local consensus set is consistent
// with the "network" consensus set.
type Gateway interface {
	// Bootstrap joins the Sia network and establishes an initial peer list.
	Bootstrap(NetAddress) error

	// AddPeer adds a peer to the Gateway's peer list. The peer
	// may be rejected. AddPeer is also an RPC.
	AddPeer(NetAddress) error

	// RemovePeer removes a peer from the Gateway's peer list.
	RemovePeer(NetAddress) error

	// RandomPeer returns a random peer from the Gateway's peer list.
	RandomPeer() (NetAddress, error)

	// RPC establishes a connection to the supplied address and writes the RPC
	// header, indicating which function will handle the connection. The
	// supplied function takes over from there.
	RPC(NetAddress, string, RPCFunc) error

	// RegisterRPC registers a function to handle incoming connections that
	// supply the given RPC ID.
	RegisterRPC(string, RPCFunc)

	// Synchronize synchronizes the local consensus set with the set of the
	// given peer.
	Synchronize(NetAddress) error

	// RelayBlock broadcasts a block to the Gateway's peers.
	RelayBlock(consensus.Block)

	// RelayTransaction broadcasts a transaction to the Gateway's peers.
	RelayTransaction(consensus.Transaction)

	// Info reports metadata about the Gateway.
	Info() GatewayInfo
}
