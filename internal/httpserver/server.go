package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/katesclau/slacker/internal/mcpauth"
)

type Server struct {
	srv         *http.Server
	log         *slog.Logger
	authService func(ctx context.Context, name string) (*mcpauth.Service, error)
}

func New(addr string, log *slog.Logger, authService func(ctx context.Context, name string) (*mcpauth.Service, error)) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	s := &Server{
		log:         log,
		authService: authService,
	}
	r.Get("/health", s.health)
	r.Route("/slacker/v1/oauth/{mcp_server}", func(rt chi.Router) {
		rt.Get("/start", s.oauthStart)
		rt.Get("/callback", s.oauthCallback)
	})

	s.srv = &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *Server) Run() error {
	s.log.Info("http server starting", "addr", s.srv.Addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf("%v", err)})
}
