package gateway

import (
	"net"
	"sync"

	"github.com/NebulousLabs/Sia/modules"
)

type rpcID [8]byte

// handlerName truncates a string to 8 bytes. If len(name) < 8, the remaining
// bytes are 0. A handlerName is specified at the beginning of each network
// call, indicating which function should handle the connection.
func handlerName(name string) (id rpcID) {
	copy(id[:], name)
	return
}

// RPC establishes a TCP connection to the NetAddress, writes the RPC
// identifier, and then hands off the connection to fn. When fn returns, the
// connection is closed.
func (g *Gateway) RPC(addr modules.NetAddress, name string, fn modules.RPCFunc) (err error) {
	// if something goes wrong, give the peer a strike
	defer func() {
		if err != nil {
			counter := g.mu.Lock()
			g.addStrike(addr)
			g.mu.Unlock(counter)
		}
	}()

	conn, err := dial(addr)
	if err != nil {
		return
	}
	defer conn.Close()
	// write header
	if err = conn.WriteObject(handlerName(name)); err != nil {
		return
	}
	err = fn(conn)
	return
}

// RegisterRPC registers a function as an RPC handler for a given identifier.
// To call an RPC, use gateway.RPC, supplying the same identifier given to
// RegisterRPC. Identifiers should always use PascalCase.
func (g *Gateway) RegisterRPC(name string, fn modules.RPCFunc) {
	counter := g.mu.Lock()
	defer g.mu.Unlock(counter)
	g.handlerMap[handlerName(name)] = fn
}

// readerRPC returns a closure that can be passed to RPC to read a
// single value.
func readerRPC(obj interface{}, maxLen uint64) modules.RPCFunc {
	return func(conn modules.NetConn) error {
		return conn.ReadObject(obj, maxLen)
	}
}

// writerRPC returns a closure that can be passed to RPC to write a
// single value.
func writerRPC(obj interface{}) modules.RPCFunc {
	return func(conn modules.NetConn) error {
		return conn.WriteObject(obj)
	}
}

// startListener creates a net.Listener on the RPC port and spawns a goroutine
// that accepts and serves connections. This goroutine will terminate when
// Close is called.
func (g *Gateway) startListener(addr string) (err error) {
	// create listener
	g.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return
	}

	go func() {
		for {
			conn, err := accept(g.listener)
			if err != nil {
				return
			}

			// it is the handler's responsibility to close the connection
			go g.threadedHandleConn(conn)
		}
	}()

	return
}

// threadedHandleConn reads header data from a connection, then routes it to the
// appropriate handler for further processing.
func (g *Gateway) threadedHandleConn(conn modules.NetConn) {
	defer conn.Close()
	var id rpcID
	if err := conn.ReadObject(&id, 8); err != nil {
		// TODO: log error
		return
	}
	// call registered handler for this ID
	counter := g.mu.RLock()
	fn, ok := g.handlerMap[id]
	g.mu.RUnlock(counter)
	if ok {
		fn(conn)
		// TODO: log error
	}
	return
}

// threadedBroadcast calls an RPC on all of the peers in the Gateway's peer
// list. The calls are run in parallel.
func (g *Gateway) threadedBroadcast(name string, fn modules.RPCFunc) {
	var wg sync.WaitGroup
	wg.Add(len(g.peers))
	counter := g.mu.RLock()
	for peer := range g.peers {
		// contact each peer in a separate thread
		go func(peer modules.NetAddress) {
			g.RPC(peer, name, fn)
			wg.Done()
		}(peer)
	}
	g.mu.RUnlock(counter)
	wg.Wait()
}
