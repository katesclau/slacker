package httpserver

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	server := chi.URLParam(r, "mcp_server")
	svc := s.authByName[server]
	if svc == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("oauth service not configured for %q", server))
		return
	}

	q := r.URL.Query()
	teamID, userID, requestID := q.Get("team_id"), q.Get("user_id"), q.Get("request_id")
	if teamID == "" || userID == "" || requestID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing query parameters team_id, user_id, request_id"))
		return
	}

	scopeHint := q.Get("scope")
	resourceMetadata := q.Get("resource_metadata")
	url, err := svc.AuthorizeURL(r.Context(), teamID, userID, requestID, scopeHint, resourceMetadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	server := chi.URLParam(r, "mcp_server")
	svc := s.authByName[server]
	if svc == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("oauth service not configured for %q", server))
		return
	}

	q := r.URL.Query()
	if providerErr := q.Get("error"); providerErr != "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("oauth provider error: %s (%s)", providerErr, q.Get("error_description")))
		return
	}

	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing code or state"))
		return
	}

	if _, err := svc.ExchangeCallback(r.Context(), code, state); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><html><body><p>OAuth authorization succeeded. You can close this tab.</p></body></html>`))
}
