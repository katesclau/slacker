package httpserver

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"html/template"
	"net/http"
	"strings"

	"github.com/katesclau/slacker/assets"
)

//go:embed templates/oauth_success.html
var oauthSuccessTemplate string

var oauthSuccessTmpl = template.Must(template.New("oauth_success").Parse(oauthSuccessTemplate))

type oauthSuccessData struct {
	LogoSrc template.URL
	Server  string
}

func writeOAuthSuccess(w http.ResponseWriter, server string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(renderOAuthSuccess(server))
}

func renderOAuthSuccess(server string) []byte {
	server = strings.TrimSpace(server)
	if server == "" {
		server = "MCP server"
	}
	var buf bytes.Buffer
	data := oauthSuccessData{
		LogoSrc: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(assets.SlackerLogoPNG)),
		Server:  server,
	}
	if err := oauthSuccessTmpl.Execute(&buf, data); err != nil {
		return []byte("<!doctype html><html><body><p>You're connected. Head back to Slack.</p></body></html>")
	}
	return buf.Bytes()
}
