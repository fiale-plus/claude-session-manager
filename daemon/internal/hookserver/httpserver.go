package hookserver

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

const DefaultHTTPPort = "19380"

// HTTPServer serves CC hook events over HTTP.
type HTTPServer struct {
	server  *http.Server
	handler *Handler
}

// NewHTTP creates an HTTP hook server on the given port.
func NewHTTP(port string, st *state.Manager) *HTTPServer {
	h := NewHandler(st)
	mux := http.NewServeMux()

	s := &HTTPServer{
		handler: h,
		server: &http.Server{
			Addr:    "127.0.0.1:" + port,
			Handler: mux,
		},
	}

	mux.HandleFunc("/hooks", s.handleHook)
	return s
}

// Serve starts the HTTP server. Blocks until closed.
func (s *HTTPServer) Serve() {
	log.Printf("http hook server listening on %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("http hook server error: %v", err)
	}
}

// Close stops the HTTP server.
func (s *HTTPServer) Close() error {
	return s.server.Close()
}

func (s *HTTPServer) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req hookRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("http hook: invalid JSON: %v (raw: %.200s)", err, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}\n"))
		return
	}

	var resp hookResponse
	switch req.HookEventName {
	case "SessionStart":
		s.handler.handleSessionStart(req)
	case "SessionEnd":
		s.handler.handleSessionEnd(req)
	case "PreToolUse":
		resp = s.handler.handlePreToolUse(req)
	default:
		log.Printf("http hook: unknown event: %s", req.HookEventName)
	}

	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(resp)
	w.Write(append(data, '\n'))
}
