// Command worker runs everything that happens outside a request: relaying the
// outbox, processing inbound webhooks, advancing SLA timers and sending
// notifications.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/filter"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/mailin"
	"github.com/armature/armature/backend/internal/notify"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/privacy"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/webhook"
	"github.com/armature/armature/backend/internal/workflow"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := observability.Setup(ctx, observability.Config{
		Service: "armature-worker", Env: cfg.Env, MetricsAddr: cfg.Telemetry.MetricsAddr,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint, SampleRatio: cfg.Telemetry.SampleRatio,
	}, log)
	if err != nil {
		return err
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := tel.Shutdown(flushCtx); err != nil {
			log.Warn("traces still in hand were not all sent", "error", err)
		}
	}()

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

	// Each background loop runs independently; if one exits with an error the
	// group cancels the rest so the process restarts as a whole.
	if err := tel.Register(observability.NewStreamCollector(rdb, events.StreamKey, events.Groups)); err != nil {
		return err
	}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return tel.Serve(ctx) })
	g.Go(func() error { return events.NewRelay(cluster, rdb, log).Run(ctx) })
	// The other direction of the git integration: an issue that moves tells
	// the pull requests about it.
	g.Go(func() error { return git.NewSync(cluster, rdb, log).Run(ctx) })

	// The service desk's clocks run out between requests, and customers are
	// told what happened to theirs; both belong here rather than in a request.
	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	issues := issue.NewService(cluster, engine, workflow.NewStore())
	deskService := desk.NewService(cluster, issues)
	g.Go(func() error { return desk.NewWatch(cluster, deskService, log).Run(ctx) })

	// The burndown's days: the plan counts, the sprint service writes.
	sprints := sprint.NewService(cluster)
	sprints.CountWith(plan.NewService(issues, sprints))
	g.Go(func() error { return sprint.NewSnapshots(cluster, sprints, log).Run(ctx) })
	// The flow's days: how many issues stood in each status, per project.
	g.Go(func() error {
		return report.NewFlowSnapshots(cluster, report.NewService(cluster, plan.NewService(issues, sprints)), log).Run(ctx)
	})

	// Files whose issue was deleted are removed from the bucket here, after
	// the fact, from the tombstones the deletion left.
	store, err := attachment.FromConfig(attachment.S3Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
	})
	if err != nil {
		return err
	}
	if _, off := store.(attachment.Unavailable); off {
		log.Info("attachment reaper is off: ARMATURE_S3_ENDPOINT is not set")
	} else {
		reaper := attachment.NewReaper(attachment.NewService(cluster, store, issues), log)
		g.Go(func() error { return reaper.Run(ctx) })
	}
	// People on ordinary issues hear through the inbox whether or not mail is
	// on; mail is a copy of the inbox when there is a relay to send it through.
	var inboxMailer mail.Mailer
	if cfg.Mail.SMTPAddr != "" {
		inboxMailer = mail.SMTPMailer{Addr: cfg.Mail.SMTPAddr, From: cfg.Mail.From}
	}
	g.Go(func() error { return notify.NewFanOut(cluster, rdb, inboxMailer, cfg.Mail.AppBaseURL, log).Run(ctx) })
	g.Go(func() error { return audit.NewConsumer(cluster, rdb, log).Run(ctx) })
	// What has served its purpose is pruned on the schedule the policy sets.
	g.Go(func() error { return privacy.NewRetention(cluster, cfg.Retention, log).Run(ctx) })
	g.Go(func() error { return notify.NewDigest(cluster, inboxMailer, cfg.Mail.AppBaseURL, log).Run(ctx) })

	// Rules run from the stream and from the clock; webhooks are queued from
	// the stream and posted from the queue.
	webhooks := webhook.NewService(cluster, log)
	hooks := webhook.NewConsumer(webhooks, rdb, log)
	g.Go(func() error { return hooks.Run(ctx) })
	g.Go(func() error { return hooks.RunSender(ctx) })
	rules := automation.NewConsumer(automation.NewService(cluster, issues, label.NewService(cluster), log).WithMailer(inboxMailer).WithWebhooks(webhooks), rdb, log)
	g.Go(func() error { return rules.Run(ctx) })
	g.Go(func() error { return rules.RunSchedules(ctx) })
	// Saved filters somebody subscribed to are mailed on their schedule.
	g.Go(func() error {
		return filter.NewSubscriptions(cluster, issues, inboxMailer, cfg.Mail.AppBaseURL, log).Run(ctx)
	})

	if cfg.Mail.SMTPAddr != "" {
		mailer := desk.SMTPMailer{Addr: cfg.Mail.SMTPAddr, From: cfg.Mail.From}
		notifier := desk.NewNotifier(cluster, rdb, mailer, cfg.Mail.AppBaseURL, log).WithInbox(cfg.Mail.Inbox)
		g.Go(func() error { return notifier.Run(ctx) })
		// Replies come back to the inbox, which is read over POP3. It needs
		// the mailer too, to tell a stranger their reply was not taken.
		switch {
		case cfg.Mail.POP3.Addr == "":
			log.Info("replies by mail are off: ARMATURE_POP3_ADDR is not set")
		case cfg.Mail.Inbox == "":
			return errors.New("ARMATURE_POP3_ADDR is set but ARMATURE_MAIL_INBOX is not: the reader needs to know which address is the desk's")
		default:
			box := mailin.POP3{Addr: cfg.Mail.POP3.Addr, User: cfg.Mail.POP3.User, Password: cfg.Mail.POP3.Password, TLS: cfg.Mail.POP3.TLS}
			reader := desk.NewInbound(cluster, deskService, issues, mailer, box,
				desk.InboundConfig{Address: cfg.Mail.Inbox, From: cfg.Mail.From, AppURL: cfg.Mail.AppBaseURL, Interval: cfg.Mail.POP3.Interval}, log)
			g.Go(func() error { return reader.Run(ctx) })
		}
	} else {
		log.Info("mail is off: ARMATURE_SMTP_ADDR is not set")
	}

	log.Info("worker started", "env", cfg.Env)
	if err := g.Wait(); err != nil {
		return err
	}
	log.Info("worker stopped cleanly")
	return nil
}
