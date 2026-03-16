// Package hookserver handles CC hook events over a Unix socket.
// Each connection sends one JSON request, receives one JSON response, then closes.
package hookserver

import (
	"log"
	"net"
	"os"

	"github.com/pchaganti/claude-session-manager/daemon/internal/pr"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

const DefaultSocket = "/tmp/csm.sock"

// Server listens on a Unix socket for CC hook events.
type Server struct {
	listener net.Listener
	state    *state.Manager
	handler  *Handler
}

// New creates a hook server.
func New(socketPath string, st *state.Manager) (*Server, error) {
	// Remove stale socket if it exists.
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	// Make socket world-writable so any CC process can connect.
	_ = os.Chmod(socketPath, 0o777)

	return &Server{
		listener: ln,
		state:    st,
		handler:  NewHandler(st),
	}, nil
}

// Serve accepts connections in a loop. Blocks until the listener is closed.
// SetPRPoller sets the PR poller for PostToolUse auto-detection.
func (s *Server) SetPRPoller(p *pr.Poller) { s.handler.SetPRPoller(p) }

func (s *Server) Serve() {
	log.Printf("hook server listening on %s", s.listener.Addr())
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Listener closed.
			return
		}
		go s.handler.Handle(conn)
	}
}

// Close stops the server and removes the socket file.
func (s *Server) Close() error {
	return s.listener.Close()
}
