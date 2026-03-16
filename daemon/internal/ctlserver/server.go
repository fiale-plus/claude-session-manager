// Package ctlserver handles TUI client connections over a Unix socket.
// Connections are persistent — clients can send multiple requests and
// subscribe to streaming state updates.
package ctlserver

import (
	"log"
	"net"
	"os"

	"github.com/pchaganti/claude-session-manager/daemon/internal/pr"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

const DefaultSocket = "/tmp/csm-ctl.sock"

// Server listens on a Unix socket for TUI client connections.
type Server struct {
	listener net.Listener
	state    *state.Manager
	prPoll   *pr.Poller
}

// New creates a control server.
func New(socketPath string, st *state.Manager, prPoll *pr.Poller) (*Server, error) {
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(socketPath, 0o777)

	return &Server{
		listener: ln,
		state:    st,
		prPoll:   prPoll,
	}, nil
}

// Serve accepts connections in a loop. Blocks until the listener is closed.
func (s *Server) Serve() {
	log.Printf("control server listening on %s", s.listener.Addr())
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		h := NewHandler(s.state, s.prPoll)
		go h.Handle(conn)
	}
}

// Close stops the server.
func (s *Server) Close() error {
	return s.listener.Close()
}
