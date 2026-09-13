// Package config loads and validates process configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved configuration for the api and worker binaries.
type Config struct {
	Env      string // development | staging | production
	HTTPAddr string

	DB    DB
	Redis Redis
	Auth  Auth
	Mail  Mail
	S3    S3
	// Render is the service that prints pages as PDF; empty turns export off.
	Render Render
	// Assistant is the model behind the palette's Ask; empty turns it off.
	Assistant Assistant
	// Telemetry is where the process says how it is doing.
	Telemetry Telemetry

	// Retention is how long each kind of personal data is kept past its use.
	Retention Retention

	LogLevel string
}

// Telemetry is the metrics listener and the trace collector. Both are off when
// their address is blank, and a blank set on purpose is honoured.
type Telemetry struct {
	// MetricsAddr serves /metrics on its own port, never through the API.
	MetricsAddr string
	// OTLPEndpoint is the collector's traces URL; empty means no traces leave.
	OTLPEndpoint string
	// SampleRatio is the share of new traces kept, 0 to 1.
	SampleRatio float64
}

// DB describes the Postgres cluster: one writable primary and zero or more
// read replicas. Replicas are optional so that a developer can run against a
// single Postgres instance without any special setup.
type DB struct {
	PrimaryURL  string
	ReplicaURLs []string
	// AdminURL connects as the role that is exempt from row level security by
	// explicit policy. Signup, login and the outbox relay legitimately operate
	// before or across tenants and use it; everything else must not. Defaults
	// to PrimaryURL when unset, which is correct for single-role setups such as
	// the test harness.
	AdminURL string

	MaxConns          int32
	MinConns          int32
	ConnMaxLifetime   time.Duration
	HealthInterval    time.Duration
	MaxReplicaLag     time.Duration
	ReplicaLagSamples int
}

// Mail is how customers are told about their requests. An empty address turns
// mail off, which is what a test wants and a demo does not.
type Mail struct {
	SMTPAddr string
	From     string
	// AppBaseURL is where the links in a mail point.
	AppBaseURL string
	// Inbox is the address replies go to, and the only address the worker
	// reads mail for. Empty means replies by mail are off.
	Inbox string
	// POP3 is the mailbox the inbox is read from.
	POP3 POP3
}

// POP3 is where the desk's replies wait. An empty address turns reading off.
type POP3 struct {
	Addr     string
	User     string
	Password string
	TLS      bool
	Interval time.Duration
}

// S3 is the bucket attachments live in. Any service that speaks the S3
// protocol will do; an empty endpoint turns attachments off, and an upload then
// says so.
type S3 struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Redis holds cache, session and job-queue connection settings.
type Redis struct {
	URL string
}

// Auth holds session and password hashing settings.
type Auth struct {
	SessionTTL      time.Duration
	SessionCookie   string
	SecureCookies   bool
	ArgonMemoryKiB  uint32
	ArgonIterations uint32
	ArgonThreads    uint8
	// ReadYourWritesTTL is how long a user's last write LSN is remembered when
	// deciding whether a read may be served from a replica.
	ReadYourWritesTTL time.Duration
	// PortalCodeCooldown is how long an address waits between portal codes.
	PortalCodeCooldown time.Duration
}

// Render names the render service, when there is one.
type Render struct {
	URL     string
	Timeout time.Duration
}

// Assistant names the model provider, when there is one.
type Assistant struct {
	URL     string
	Key     string
	Model   string
	Timeout time.Duration
}

// Load reads configuration from the environment, applies defaults and validates
// the result. It returns every problem it finds at once rather than the first.
func Load() (Config, error) {
	defaults := DefaultRetention()
	c := Config{
		Env:      env("ARMATURE_ENV", "development"),
		HTTPAddr: env("ARMATURE_HTTP_ADDR", ":8080"),
		LogLevel: env("ARMATURE_LOG_LEVEL", "info"),
		DB: DB{
			PrimaryURL:        env("ARMATURE_DB_PRIMARY_URL", ""),
			ReplicaURLs:       splitList(env("ARMATURE_DB_REPLICA_URLS", "")),
			AdminURL:          env("ARMATURE_DB_ADMIN_URL", ""),
			MaxConns:          int32(envInt("ARMATURE_DB_MAX_CONNS", 20)),
			MinConns:          int32(envInt("ARMATURE_DB_MIN_CONNS", 2)),
			ConnMaxLifetime:   envDur("ARMATURE_DB_CONN_MAX_LIFETIME", time.Hour),
			HealthInterval:    envDur("ARMATURE_DB_HEALTH_INTERVAL", 5*time.Second),
			MaxReplicaLag:     envDur("ARMATURE_DB_MAX_REPLICA_LAG", 2*time.Second),
			ReplicaLagSamples: envInt("ARMATURE_DB_REPLICA_LAG_SAMPLES", 3),
		},
		Redis: Redis{
			URL: env("ARMATURE_REDIS_URL", "redis://localhost:6379/0"),
		},
		Telemetry: Telemetry{
			MetricsAddr:  envOrBlank("ARMATURE_METRICS_ADDR", ":9090"),
			OTLPEndpoint: env("ARMATURE_OTEL_ENDPOINT", ""),
			SampleRatio:  envFloat("ARMATURE_OTEL_SAMPLE_RATIO", 1),
		},
		Render: Render{
			URL:     strings.TrimSuffix(env("ARMATURE_RENDER_URL", ""), "/"),
			Timeout: envDur("ARMATURE_RENDER_TIMEOUT", 20*time.Second),
		},
		Assistant: Assistant{
			URL:     strings.TrimSuffix(env("ARMATURE_ASSISTANT_URL", ""), "/"),
			Key:     env("ARMATURE_ASSISTANT_KEY", ""),
			Model:   env("ARMATURE_ASSISTANT_MODEL", "claude-sonnet-5"),
			Timeout: envDur("ARMATURE_ASSISTANT_TIMEOUT", 25*time.Second),
		},
		Mail: Mail{
			SMTPAddr:   env("ARMATURE_SMTP_ADDR", ""),
			From:       env("ARMATURE_MAIL_FROM", "Armature <no-reply@armature.test>"),
			AppBaseURL: env("ARMATURE_APP_URL", "http://localhost:5173"),
			Inbox:      env("ARMATURE_MAIL_INBOX", ""),
			POP3: POP3{
				Addr:     env("ARMATURE_POP3_ADDR", ""),
				User:     env("ARMATURE_POP3_USER", ""),
				Password: env("ARMATURE_POP3_PASSWORD", ""),
				TLS:      envBool("ARMATURE_POP3_TLS", false),
				Interval: envDur("ARMATURE_POP3_INTERVAL", 30*time.Second),
			},
		},
		S3: S3{
			Endpoint:  env("ARMATURE_S3_ENDPOINT", ""),
			Bucket:    env("ARMATURE_S3_BUCKET", "armature-attachments"),
			AccessKey: env("ARMATURE_S3_ACCESS_KEY", ""),
			SecretKey: env("ARMATURE_S3_SECRET_KEY", ""),
			Region:    env("ARMATURE_S3_REGION", "us-east-1"),
			UseSSL:    envBool("ARMATURE_S3_USE_SSL", false),
		},
		Retention: Retention{
			Sessions:          envDur("ARMATURE_RETAIN_SESSIONS", defaults.Sessions),
			PortalCodes:       envDur("ARMATURE_RETAIN_PORTAL_CODES", defaults.PortalCodes),
			Invites:           envDur("ARMATURE_RETAIN_INVITES", defaults.Invites),
			APITokens:         envDur("ARMATURE_RETAIN_API_TOKENS", defaults.APITokens),
			OIDCLogins:        envDur("ARMATURE_RETAIN_OIDC_LOGINS", defaults.OIDCLogins),
			Notifications:     envDur("ARMATURE_RETAIN_NOTIFICATIONS", defaults.Notifications),
			Outbox:            envDur("ARMATURE_RETAIN_OUTBOX", defaults.Outbox),
			WebhookDeliveries: envDur("ARMATURE_RETAIN_WEBHOOK_DELIVERIES", defaults.WebhookDeliveries),
			AutomationRuns:    envDur("ARMATURE_RETAIN_AUTOMATION_RUNS", defaults.AutomationRuns),
			InboundMail:       envDur("ARMATURE_RETAIN_INBOUND_MAIL", defaults.InboundMail),
			ImportJobs:        envDur("ARMATURE_RETAIN_IMPORT_JOBS", defaults.ImportJobs),
			Audit:             envDur("ARMATURE_RETAIN_AUDIT", defaults.Audit),
		},
		Auth: Auth{
			SessionTTL:         envDur("ARMATURE_SESSION_TTL", 720*time.Hour),
			SessionCookie:      env("ARMATURE_SESSION_COOKIE", "armature_session"),
			SecureCookies:      envBool("ARMATURE_SECURE_COOKIES", false),
			ArgonMemoryKiB:     uint32(envInt("ARMATURE_ARGON_MEMORY_KIB", 64*1024)),
			ArgonIterations:    uint32(envInt("ARMATURE_ARGON_ITERATIONS", 3)),
			ArgonThreads:       uint8(envInt("ARMATURE_ARGON_THREADS", 4)),
			ReadYourWritesTTL:  envDur("ARMATURE_READ_YOUR_WRITES_TTL", 30*time.Second),
			PortalCodeCooldown: envDur("ARMATURE_PORTAL_CODE_COOLDOWN", time.Minute),
		},
	}

	var problems []string
	if c.DB.PrimaryURL == "" {
		problems = append(problems, "ARMATURE_DB_PRIMARY_URL is required")
	}
	switch c.Env {
	case "development", "staging", "production":
	default:
		problems = append(problems, fmt.Sprintf("ARMATURE_ENV %q must be development, staging or production", c.Env))
	}
	if c.DB.MinConns > c.DB.MaxConns {
		problems = append(problems, "ARMATURE_DB_MIN_CONNS must not exceed ARMATURE_DB_MAX_CONNS")
	}
	// Staging is a deployment people reach over a network, so it is held to
	// what production is held to; development is the one that runs locally.
	if c.Env != "development" && !c.Auth.SecureCookies {
		problems = append(problems, "ARMATURE_SECURE_COOKIES must be true outside development")
	}
	if c.Telemetry.SampleRatio < 0 || c.Telemetry.SampleRatio > 1 {
		problems = append(problems, "ARMATURE_OTEL_SAMPLE_RATIO must be between 0 and 1")
	}
	if c.DB.AdminURL == "" {
		c.DB.AdminURL = c.DB.PrimaryURL
	}
	// A bucket without credentials would only be discovered when the first
	// request to it came back forbidden, long after startup.
	if c.S3.Endpoint != "" {
		if c.S3.AccessKey == "" {
			problems = append(problems, "ARMATURE_S3_ACCESS_KEY is required when ARMATURE_S3_ENDPOINT is set")
		}
		if c.S3.SecretKey == "" {
			problems = append(problems, "ARMATURE_S3_SECRET_KEY is required when ARMATURE_S3_ENDPOINT is set")
		}
		if strings.TrimSpace(c.S3.Bucket) == "" {
			problems = append(problems, "ARMATURE_S3_BUCKET must not be blank")
		}
	}
	if len(problems) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return c, nil
}

func (c Config) IsProduction() bool { return c.Env == "production" }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// envOrBlank is env for a setting where blank means off: set to nothing, it
// stays nothing rather than falling back to the default.
func envOrBlank(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// splitList parses a comma separated list, trimming blanks.
func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

const day = 24 * time.Hour

// Retention says how long each kind of row is kept past the point it stopped
// being needed. Zero keeps a kind forever, which the documentation discourages.
type Retention struct {
	// Sessions past their expiry. They hold an address and a user agent.
	Sessions time.Duration
	// Portal codes past their expiry. They hold an address in clear.
	PortalCodes time.Duration
	// Invitations past their expiry, accepted or not.
	Invites time.Duration
	// API tokens past their expiry.
	APITokens time.Duration
	// Sign-in handshakes with the identity provider past their expiry.
	OIDCLogins time.Duration
	// Notifications, from when they were made.
	Notifications time.Duration
	// Outbox rows already published, from when they were published.
	Outbox time.Duration
	// Webhook deliveries, from when they were queued.
	WebhookDeliveries time.Duration
	// Automation runs, from when they started.
	AutomationRuns time.Duration
	// The record of mail read from the desk's box, from when it arrived.
	InboundMail time.Duration
	// Import jobs and their reports, from when they were started.
	ImportJobs time.Duration
	// The audit log, from when the row was written.
	Audit time.Duration
}

// DefaultRetention is what a fresh installation keeps: short for what only served
// a moment, a season for what somebody might look back at, a year for the
// record of who did what.
func DefaultRetention() Retention {
	return Retention{
		Sessions:          1 * day,
		PortalCodes:       1 * day,
		Invites:           30 * day,
		APITokens:         30 * day,
		OIDCLogins:        1 * day,
		Notifications:     180 * day,
		Outbox:            30 * day,
		WebhookDeliveries: 30 * day,
		AutomationRuns:    90 * day,
		InboundMail:       90 * day,
		ImportJobs:        90 * day,
		Audit:             365 * day,
	}
}
