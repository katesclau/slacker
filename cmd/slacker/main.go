package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/katesclau/slacker/internal/agents"
	"github.com/katesclau/slacker/internal/config"
	"github.com/katesclau/slacker/internal/httpserver"
	"github.com/katesclau/slacker/internal/logger"
	"github.com/katesclau/slacker/internal/mcpauth"
	"github.com/katesclau/slacker/internal/memory"
	"github.com/katesclau/slacker/internal/objectstore/minio"
	"github.com/katesclau/slacker/internal/openaiadapter"
	"github.com/katesclau/slacker/internal/prompt"
	slackruntime "github.com/katesclau/slacker/internal/slack"
	"github.com/katesclau/slacker/internal/store/postgres"
	"github.com/katesclau/slacker/internal/tooling/blockkit"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.App.LogLevel)

	dbPool, err := postgres.NewPool(ctx, cfg.Postgres.DSN)
	if err != nil {
		log.Error("postgres init failed", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()
	repo := postgres.NewRepository(dbPool)

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Error("redis init failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	memorySvc := memory.NewService(dbPool, redisClient)
	blockRegistry := blockkit.NewRegistry()
	blockTools := blockkit.NewTools(blockRegistry)

	minioStore, err := minio.New(ctx, minio.Config{
		Endpoint:  cfg.MinIO.Endpoint,
		AccessKey: cfg.MinIO.AccessKey,
		SecretKey: cfg.MinIO.SecretKey,
		UseSSL:    cfg.MinIO.UseSSL,
		Bucket:    cfg.MinIO.Bucket,
	})
	if err != nil {
		log.Error("minio init failed", "error", err)
		os.Exit(1)
	}
	promptSvc := prompt.NewService(repo, minioStore)
	_ = promptSvc // keeps prompt service instantiated for startup parity and health.

	cipher, err := mcpauth.NewTokenCipher(cfg.Security.TokenEncryptionKey)
	if err != nil {
		log.Error("token cipher init failed", "error", err)
		os.Exit(1)
	}
	modelAdapter, err := openaiadapter.New(cfg.OpenAI.APIKey, cfg.OpenAI.DefaultModel)
	if err != nil {
		log.Error("failed to initialize adk model adapter", "error", err)
		os.Exit(1)
	}
	tokenResolver := mcpauth.TokenResolver{
		Store:  repo,
		Cipher: cipher,
	}
	agentRuntime, err := agents.NewRuntime("slacker", modelAdapter, repo, blockRegistry, tokenResolver)
	if err != nil {
		log.Error("failed to initialize adk runtime", "error", err)
		os.Exit(1)
	}

	httpSrv := httpserver.New(
		netAddr(cfg.App.Port),
		log,
		newMCPAuthServiceResolver(cfg, repo, cipher, log),
	)

	slackRuntime := slackruntime.New(slackruntime.Config{
		AppToken:      cfg.Slack.AppToken,
		BotToken:      cfg.Slack.BotToken,
		ChatCommand:   cfg.Slack.ChatCommand,
		ConfigCommand: cfg.Slack.ConfigCommand,
		AdminUsers:    cfg.Slack.AdminUsers,
		PublicBaseURL: cfg.App.PublicBaseURL,
	}, log, repo, memorySvc, blockTools, agentRuntime)

	errCh := make(chan error, 2)
	go func() { errCh <- httpSrv.Run() }()
	go func() { errCh <- slackRuntime.Start(ctx) }()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case runErr := <-errCh:
		if runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
			log.Error("runtime failure", "error", runErr)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown failed", "error", err)
	}
}

func newMCPAuthServiceResolver(cfg config.Config, repo *postgres.Repository, cipher *mcpauth.TokenCipher, log *slog.Logger) func(ctx context.Context, name string) (*mcpauth.Service, error) {
	var mu sync.RWMutex
	cache := map[string]*mcpauth.Service{}
	return func(ctx context.Context, name string) (*mcpauth.Service, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, nil
		}
		mu.RLock()
		cached := cache[name]
		mu.RUnlock()
		if cached != nil {
			return cached, nil
		}
		svc, err := buildMCPAuthService(ctx, cfg, repo, cipher, log, name)
		if err != nil {
			return nil, err
		}
		if svc == nil {
			return nil, nil
		}
		mu.Lock()
		if existing := cache[name]; existing != nil {
			mu.Unlock()
			return existing, nil
		}
		cache[name] = svc
		mu.Unlock()
		return svc, nil
	}
}

func buildMCPAuthService(ctx context.Context, cfg config.Config, repo *postgres.Repository, cipher *mcpauth.TokenCipher, log *slog.Logger, targetName string) (*mcpauth.Service, error) {
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	for _, srv := range servers {
		if strings.TrimSpace(srv.Name) != strings.TrimSpace(targetName) {
			continue
		}
		scopes := splitScopesCSV(srv.ScopesCSV)
		clientSecret := srv.ClientSecretEnc
		if maybePlainCiphertext(clientSecret) {
			if dec, decErr := cipher.DecryptFromBase64(clientSecret); decErr == nil {
				clientSecret = dec
			}
		}

		return &mcpauth.Service{
			MCPServer:     srv.Name,
			ResourceURL:   srv.ResourceURL,
			PublicBaseURL: cfg.App.PublicBaseURL,
			StateHMACKey:  cfg.Security.OAuthStateHMACKey,
			Store:         repo,
			Cipher:        cipher,
			Registration: mcpauth.RegistrationConfig{
				ClientID:                  srv.ClientID,
				ClientSecret:              clientSecret,
				Scopes:                    scopes,
				AuthorizationServerIssuer: srv.IssuerURL,
				Mode:                      strings.ToLower(strings.TrimSpace(srv.AuthMode)),
				ClientName:                strings.TrimSpace(srv.ClientName),
			},
		}, nil
	}
	log.Debug("mcp auth service not found", "mcp_server", targetName)
	return nil, nil
}

func splitScopesCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func maybePlainCiphertext(v string) bool {
	return strings.TrimSpace(v) != ""
}

func netAddr(port int) string {
	return ":" + strconv.Itoa(port)
}
