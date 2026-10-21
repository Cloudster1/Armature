package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// runBootstrap is `seed bootstrap`, the chart's bootstrap Job: unlike the
// demo seed it runs in production, and it only sets up who may get in.
func runBootstrap() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, false)
	slog.SetDefault(log)

	in, err := bootstrapInput(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()

	svc := auth.NewService(cluster, auth.DefaultPasswordParams(), cfg.Auth.SessionTTL).NamedInstallation(cfg.Auth.InstallationName)
	res, err := svc.Bootstrap(ctx, in)
	if err != nil {
		return err
	}
	switch {
	case res.OwnerCreated:
		log.Info("set the installation up", "owner", in.AdminEmail)
	case res.AdminCreated:
		log.Info("added the administrator to the installation", "admin", in.AdminEmail)
	default:
		log.Info("the administrator is in already, and keeps their password", "admin", in.AdminEmail)
	}
	for _, email := range res.MembersAdded {
		log.Info("let in ahead of their first sign-in", "member", email)
	}
	return nil
}

// bootstrapInput reads the Job's environment: ARMATURE_BOOTSTRAP_ADMIN_EMAIL,
// _PASSWORD and _NAME, and ARMATURE_BOOTSTRAP_MEMBERS as email=role,...
func bootstrapInput(getenv func(string) string) (auth.BootstrapInput, error) {
	in := auth.BootstrapInput{
		AdminEmail:    strings.TrimSpace(getenv("ARMATURE_BOOTSTRAP_ADMIN_EMAIL")),
		AdminName:     strings.TrimSpace(getenv("ARMATURE_BOOTSTRAP_ADMIN_NAME")),
		AdminPassword: getenv("ARMATURE_BOOTSTRAP_ADMIN_PASSWORD"),
	}
	if in.AdminEmail == "" || in.AdminPassword == "" {
		return in, errors.New("ARMATURE_BOOTSTRAP_ADMIN_EMAIL and ARMATURE_BOOTSTRAP_ADMIN_PASSWORD must both be set; the chart sets them from bootstrap.admin")
	}
	for _, pair := range strings.Split(getenv("ARMATURE_BOOTSTRAP_MEMBERS"), ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		email, role, ok := strings.Cut(pair, "=")
		if !ok || email == "" {
			return in, fmt.Errorf("ARMATURE_BOOTSTRAP_MEMBERS has %q; write each member as email=role", pair)
		}
		in.Members = append(in.Members, auth.BootstrapMember{Email: email, Role: auth.OrgRole(role)})
	}
	return in, nil
}
