// Command api serves the HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/arrange"
	"github.com/armature/armature/backend/internal/assistant"
	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bulk"
	"github.com/armature/armature/backend/internal/calendar"
	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/filter"
	"github.com/armature/armature/backend/internal/freshness"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/httpapi"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/notify"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/privacy"
	"github.com/armature/armature/backend/internal/profile"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/render"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/version"
	"github.com/armature/armature/backend/internal/webhook"
	"github.com/armature/armature/backend/internal/workflow"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// The container health check runs the binary itself, so it needs no curl in
	// the image and no drift between what the check probes and what we serve.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck()
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := observability.Setup(ctx, telemetryConfig(cfg, "armature-api"), log)
	if err != nil {
		return err
	}

	cluster, err := db.Open(ctx, cfg.DB, log, db.WithQueryTracer(tel.QueryTracer))
	if err != nil {
		return err
	}
	defer cluster.Close()
	// Outside development the tenant wall is the database's to keep, and a
	// role that ignores it would make every policy decoration.
	if cfg.Env != "development" {
		if err := cluster.RefuseSuperuser(ctx); err != nil {
			return err
		}
	}
	if err := tel.Register(observability.NewClusterCollector(cluster)); err != nil {
		return err
	}

	redisOpts, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return fmt.Errorf("parse redis url: %w", err)
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}

	passwordParams := auth.PasswordParams{
		MemoryKiB:   cfg.Auth.ArgonMemoryKiB,
		Iterations:  cfg.Auth.ArgonIterations,
		Parallelism: cfg.Auth.ArgonThreads,
		SaltLength:  16,
		KeyLength:   32,
	}

	// One engine and one store, shared by every request: both are stateless.
	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	workflowStore := workflow.NewStore()

	issues := issue.NewService(cluster, engine, workflowStore)

	// Sprints and the plan need each other: the plan draws a project's sprints,
	// and completing a sprint records what the plan counted. Wiring it here in
	// two steps says so plainly.
	sprints := sprint.NewService(cluster)
	plans := plan.NewService(issues, sprints)
	milestones := milestone.NewService(cluster)
	plans.WithMilestones(milestones)
	sprints.CountWith(plans)
	plans.WithTeams(team.NewService(cluster))

	// The board provisioner gives every new project a board in the same
	// transaction that creates the project.
	// Every new project gets a board and a dashboard in the transaction that
	// makes it.
	projects := project.NewService(cluster, board.Provisioner{}, report.Provisioner{})
	workflowAdmin := workflow.NewAdmin(cluster, workflowStore).WithRegistry(engine.Registry()).WithObserver(board.Follower{})
	// The desk observes issues for its clocks, so it is made before anything
	// that creates or moves one is served.
	deskService := desk.NewService(cluster, issues).RepliesByMail(cfg.Mail.POP3.Addr != "")

	// The portal's codes are mailed by the API itself, while the person waits.
	// Without SMTP the door says mail is off rather than pretending.
	var mailer desk.Mailer
	if cfg.Mail.SMTPAddr != "" {
		mailer = desk.SMTPMailer{Addr: cfg.Mail.SMTPAddr, From: cfg.Mail.From}
	}

	// Attachments live in a bucket. Making sure it exists at startup is what a
	// fresh development stack needs; production has made it by hand.
	store, err := attachmentStore(ctx, cfg.S3, log)
	if err != nil {
		return err
	}

	attachments := attachment.NewService(cluster, store, issues)
	deskService.WithAttachments(attachments)

	accounts := auth.NewService(cluster, passwordParams, cfg.Auth.SessionTTL).
		PortalCodeCooldown(cfg.Auth.PortalCodeCooldown).
		Signups(auth.SignupPolicy(cfg.Auth.Signup))
	labels := label.NewService(cluster)
	webhooks := webhook.NewService(cluster, log)
	server := &httpapi.Server{
		Auth:       accounts,
		Profiles:   profile.NewService(store, accounts),
		Privacy:    privacy.NewService(cluster).WithAvatars(profile.NewService(store, accounts)),
		Mailer:     mailer,
		Fields:     field.NewService(cluster),
		Arrange:    arrange.NewService(cluster),
		Labels:     labels,
		Notify:     notify.NewService(cluster),
		Automation: automation.NewService(cluster, issues, labels, log).WithMailer(mailer).WithWebhooks(webhooks),
		Webhooks:   webhooks,
		Filters:    filter.NewService(cluster),
		Audit:      audit.NewService(cluster),
		Calendar:   calendar.NewService(cluster),
		Bulk:       bulk.NewService(issues, labels),
		CSV: csvio.NewService(cluster, issues, labels, field.NewService(cluster), accounts).
			WithPlanning(sprints, version.NewService(cluster), component.NewService(cluster), team.NewService(cluster)),
		Attachments: attachments,
		Projects:    projects,
		Templates:   template.NewService(cluster, projects, workflowAdmin).WithDesk(deskService),
		Git:         git.NewService(cluster, issues),
		Desk:        deskService,
		Reports:     report.NewService(cluster, plans).WithSprints(sprints),
		Issues:      issues,
		Boards:      board.NewService(cluster, issues),
		Plans:       plans,
		Sprints:     sprints,
		Milestones:  milestones,
		Versions:    version.NewService(cluster),
		Components:  component.NewService(cluster),
		Teams:       team.NewService(cluster),
		Perms:       perm.NewStore(cluster),
		OIDC:        oidc.NewService(cluster, oidcRedirectURL()).WithHTTPClient(oidc.Backchannel(oidc.ParseRewrites(os.Getenv("ARMATURE_OIDC_BACKCHANNEL")))),
		AppBaseURL:  appBaseURL(),
		Renderer:    renderer(cfg.Render),
		Assistant:   asker(cfg.Assistant),
		Workflow:    &httpapi.WorkflowDeps{Engine: engine, Store: workflowStore, Admin: workflowAdmin},
		DB:          cluster,
		Telemetry:   tel,
		Fresh:       freshness.NewRedisTracker(rdb, cfg.Auth.ReadYourWritesTTL),
		Log:         log,
		Secure:      cfg.Auth.SecureCookies,
		CookieName:  cfg.Auth.SessionCookie,
		SessionTTL:  cfg.Auth.SessionTTL,
	}
	// In development the dev server proxies from an origin of its own, so the
	// origin check would refuse every write it forwards.
	server.CheckOrigin = cfg.IsProduction()
	proxies, err := trustedProxies(os.Getenv("ARMATURE_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	server.TrustedProxies = proxies

	origins := splitList(os.Getenv("ARMATURE_CORS_ORIGINS"))
	if len(origins) == 0 && !cfg.IsProduction() {
		// The Vite dev server runs on a different port from the API.
		origins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(origins),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Exactly one value is ever sent on this channel. Closing it instead would
	// make the select below receive a nil zero value and report a clean exit
	// for what was actually a failure to serve.
	serveErr := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()
	// The metrics port is the process' own business and ends with it.
	go func() {
		if err := tel.Serve(ctx); err != nil {
			log.Warn("metrics listener stopped", "error", err)
		}
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return errors.New("http server stopped serving without being asked to")
	case <-ctx.Done():
		log.Info("shutdown requested, draining connections")
	}

	// Give in-flight requests a chance to finish before the pools close.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := tel.Shutdown(shutdownCtx); err != nil {
		log.Warn("traces still in hand were not all sent", "error", err)
	}
	log.Info("api stopped cleanly")
	return nil
}

// telemetryConfig is the process' telemetry settings under the binary's name.
func telemetryConfig(cfg config.Config, service string) observability.Config {
	return observability.Config{
		Service:      service,
		Env:          cfg.Env,
		MetricsAddr:  cfg.Telemetry.MetricsAddr,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
		SampleRatio:  cfg.Telemetry.SampleRatio,
	}
}

// attachmentStore connects to the configured bucket, or returns the store that
// refuses with the setting to fix when there is none.
func attachmentStore(ctx context.Context, cfg config.S3, log *slog.Logger) (attachment.Store, error) {
	s3cfg := attachment.S3Config{
		Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey, Region: cfg.Region, UseSSL: cfg.UseSSL,
	}
	if !s3cfg.Configured() {
		log.Warn("attachments are off: ARMATURE_S3_ENDPOINT is not set")
		return attachment.Unavailable{}, nil
	}
	store, err := attachment.NewS3(s3cfg)
	if err != nil {
		return nil, err
	}
	if err := store.EnsureBucket(ctx); err != nil {
		return nil, fmt.Errorf("attachment bucket: %w", err)
	}
	return store, nil
}

// healthcheck probes the local readiness endpoint and exits non-zero if it is
// not ready, which is exactly what the container runtime wants.
func healthcheck() error {
	addr := os.Getenv("ARMATURE_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned %s", resp.Status)
	}
	return nil
}

// trustedProxies reads the proxies whose X-Forwarded-For is believed, as CIDRs
// or single addresses separated by commas.
func trustedProxies(v string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range splitList(v) {
		if prefix, err := netip.ParsePrefix(item); err == nil {
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("ARMATURE_TRUSTED_PROXIES: %q is neither an address nor a CIDR", item)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// appBaseURL is where a browser is sent back to after signing in somewhere
// else. The API cannot infer it: the request it is answering comes from the
// identity provider's redirect, not from the application.
func appBaseURL() string {
	if url := strings.TrimSuffix(os.Getenv("ARMATURE_APP_URL"), "/"); url != "" {
		return url
	}
	return "http://localhost:5173"
}

// oidcRedirectURL is the address the provider is told to send people back to.
// It has to match what is registered with the provider exactly, so it is
// configuration rather than something assembled from the request.
func oidcRedirectURL() string {
	if url := os.Getenv("ARMATURE_OIDC_REDIRECT_URL"); url != "" {
		return url
	}
	return "http://localhost:8080/api/v1/auth/oidc/callback"
}

// asker is the model provider when one is named, else the answer that there is none.
func asker(cfg config.Assistant) assistant.Asker {
	if cfg.URL == "" {
		return assistant.Unavailable{}
	}
	return assistant.HTTPAsker{URL: cfg.URL, Key: cfg.Key, Model: cfg.Model, Timeout: cfg.Timeout}
}

// renderer is the render service when one is named, else the answer that there is none.
func renderer(cfg config.Render) render.Renderer {
	if cfg.URL == "" {
		return render.Unavailable{}
	}
	return render.HTTPRenderer{URL: cfg.URL, Timeout: cfg.Timeout}
}
