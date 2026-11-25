package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/research"
)

type Server struct {
	runtime *research.Runtime
	mux     *http.ServeMux
	runMu   sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan error
}

func New(runtime *research.Runtime) *Server {
	server := &Server{runtime: runtime, mux: http.NewServeMux(), done: make(chan error, 1)}
	server.mux.HandleFunc("POST /api/v1/run/start", server.start)
	server.mux.HandleFunc("POST /api/v1/run/stop", server.stop)
	server.mux.HandleFunc("GET /api/v1/run/status", server.status)
	server.mux.HandleFunc("GET /api/v1/agents", server.agents)
	server.mux.HandleFunc("GET /api/v1/tasks", server.tasks)
	server.mux.HandleFunc("GET /api/v1/budget", server.budget)
	server.mux.HandleFunc("GET /api/v1/events", server.events)
	server.mux.HandleFunc("GET /api/v1/events/stream", server.stream)
	server.mux.HandleFunc("GET /api/v1/report", server.report)
	return server
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-AutoResearch-API-Version", "1")
	s.mux.ServeHTTP(writer, request)
}

func (s *Server) start(writer http.ResponseWriter, request *http.Request) {
	s.runMu.Lock()
	if s.started || s.runtime.Status().State != research.StateCreated {
		s.runMu.Unlock()
		writeError(writer, http.StatusConflict, "run has already started")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.started = true
	s.done = make(chan error, 1)
	s.runMu.Unlock()
	go func() { s.done <- s.runtime.Run(ctx) }()
	writeJSON(writer, http.StatusAccepted, map[string]string{"status": "starting"})
}

func (s *Server) Shutdown(ctx context.Context) {
	s.runMu.Lock()
	cancel := s.cancel
	started := s.started
	done := s.done
	s.runMu.Unlock()
	if !started {
		return
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (s *Server) stop(writer http.ResponseWriter, _ *http.Request) {
	s.runMu.Lock()
	cancel := s.cancel
	s.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(writer, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (s *Server) status(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, s.runtime.Status())
}

func (s *Server) agents(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"agents": s.runtime.Agents()})
}

func (s *Server) tasks(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"tasks": s.runtime.Tasks()})
}

func (s *Server) budget(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, s.runtime.Status().Budget)
}

func (s *Server) events(writer http.ResponseWriter, request *http.Request) {
	after, err := eventOffset(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	all := s.runtime.Events()
	filtered := make([]research.Event, 0, len(all))
	for _, event := range all {
		if event.Sequence > after {
			filtered = append(filtered, event)
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"events": filtered})
}

func (s *Server) stream(writer http.ResponseWriter, request *http.Request) {
	after, err := eventOffset(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, "streaming is unsupported")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, event := range s.runtime.Events() {
			if event.Sequence <= after {
				continue
			}
			payload, _ := json.Marshal(event)
			fmt.Fprintf(writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, payload)
			after = event.Sequence
			flusher.Flush()
		}
		state := s.runtime.Status().State
		if state == research.StateCompleted || state == research.StateFailed || state == research.StateCancelled {
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) report(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"report": s.runtime.Report()})
}

func eventOffset(request *http.Request) (uint64, error) {
	raw := request.URL.Query().Get("after")
	if raw == "" {
		raw = request.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		return 0, nil
	}
	offset, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, errors.New("after must be an unsigned event sequence")
	}
	return offset, nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"message": message}})
}
