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
	authServices := buildMCPAuthServices(ctx, cfg, repo, cipher, log)
	mcpServers, err := repo.ListMCPServers(ctx)
	if err != nil {
		log.Error("failed to load mcp servers", "error", err)
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
	agentRuntime, err := agents.NewRuntime("slacker", modelAdapter, repo, blockRegistry, mcpServers, tokenResolver)
	if err != nil {
		log.Error("failed to initialize adk runtime", "error", err)
		os.Exit(1)
	}

	httpSrv := httpserver.New(
		netAddr(cfg.App.Port),
		log,
		authServices,
	)

	slackRuntime := slackruntime.New(slackruntime.Config{
		AppToken:      cfg.Slack.AppToken,
		BotToken:      cfg.Slack.BotToken,
		ChatCommand:   cfg.Slack.ChatCommand,
		ConfigCommand: cfg.Slack.ConfigCommand,
		AdminUsers:    cfg.Slack.AdminUsers,
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

func buildMCPAuthServices(ctx context.Context, cfg config.Config, repo *postgres.Repository, cipher *mcpauth.TokenCipher, log *slog.Logger) map[string]*mcpauth.Service {
	services := map[string]*mcpauth.Service{}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		log.Warn("could not load mcp servers from db", "error", err)
		return services
	}
	for _, srv := range servers {
		scopes := splitScopesCSV(srv.ScopesCSV)
		clientSecret := srv.ClientSecretEnc
		if maybePlainCiphertext(clientSecret) {
			if dec, decErr := cipher.DecryptFromBase64(clientSecret); decErr == nil {
				clientSecret = dec
			}
		}

		services[srv.Name] = &mcpauth.Service{
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
			},
		}
	}
	return services
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
