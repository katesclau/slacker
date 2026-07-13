package httpserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/katesclau/slacker/internal/mcpauth"
)

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	server := chi.URLParam(r, "mcp_server")
	s.log.Debug("oauth start request received", "mcp_server", server, "path", r.URL.Path)
	svc, err := s.lookupAuthService(r.Context(), server)
	if err != nil {
		s.log.Error("oauth start service lookup failed", "mcp_server", server, "error", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if svc == nil {
		s.log.Debug("oauth start service missing", "mcp_server", server)
		writeError(w, http.StatusNotFound, fmt.Errorf("oauth service not configured for %q", server))
		return
	}

	q := r.URL.Query()
	teamID, userID, requestID := q.Get("team_id"), q.Get("user_id"), q.Get("request_id")
	if teamID == "" || userID == "" || requestID == "" {
		s.log.Debug("oauth start missing query params", "mcp_server", server, "team_id_present", teamID != "", "user_id_present", userID != "", "request_id_present", requestID != "")
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing query parameters team_id, user_id, request_id"))
		return
	}
	s.log.Debug("oauth start params parsed", "mcp_server", server, "team_id", teamID, "user_id", userID, "request_id", requestID)

	scopeHint := q.Get("scope")
	resourceMetadata := q.Get("resource_metadata")
	s.log.Debug("oauth start building authorize url", "mcp_server", server, "scope_hint", scopeHint, "resource_metadata_url_present", resourceMetadata != "")
	url, err := svc.AuthorizeURL(r.Context(), teamID, userID, requestID, scopeHint, resourceMetadata)
	if err != nil {
		s.log.Error("oauth start authorize url failed", "mcp_server", server, "error", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Debug("oauth start redirecting", "mcp_server", server, "redirect_url", url)
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	server := chi.URLParam(r, "mcp_server")
	s.log.Debug("oauth callback request received", "mcp_server", server, "path", r.URL.Path)
	svc, err := s.lookupAuthService(r.Context(), server)
	if err != nil {
		s.log.Error("oauth callback service lookup failed", "mcp_server", server, "error", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if svc == nil {
		s.log.Debug("oauth callback service missing", "mcp_server", server)
		writeError(w, http.StatusNotFound, fmt.Errorf("oauth service not configured for %q", server))
		return
	}

	q := r.URL.Query()
	if providerErr := q.Get("error"); providerErr != "" {
		s.log.Debug("oauth callback provider error", "mcp_server", server, "provider_error", providerErr, "provider_error_description", q.Get("error_description"))
		writeError(w, http.StatusBadRequest, fmt.Errorf("oauth provider error: %s (%s)", providerErr, q.Get("error_description")))
		return
	}

	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		s.log.Debug("oauth callback missing code/state", "mcp_server", server, "code_present", code != "", "state_present", state != "")
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing code or state"))
		return
	}
	s.log.Debug("oauth callback exchanging code", "mcp_server", server, "code_len", len(code), "state_len", len(state))

	st, err := svc.ExchangeCallback(r.Context(), code, state)
	if err != nil {
		s.log.Error("oauth callback exchange failed", "mcp_server", server, "error", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Debug("oauth callback exchange succeeded", "mcp_server", server)
	if s.oauthResume != nil && st != nil {
		go func(state mcpauth.OAuthState) {
			resumeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.oauthResume(resumeCtx, state); err != nil {
				s.log.Error("oauth resume failed",
					"mcp_server", state.MCPServer,
					"team_id", state.SlackTeamID,
					"user_id", state.SlackUserID,
					"request_id", state.RequestID,
					"error", err,
				)
			}
		}(*st)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><html><body><p>OAuth authorization succeeded. Slacker will resume the original conversation if one is waiting.</p></body></html>`))
}

func (s *Server) lookupAuthService(ctx context.Context, name string) (*mcpauth.Service, error) {
	if s.authService == nil {
		return nil, nil
	}
	return s.authService(ctx, name)
}
