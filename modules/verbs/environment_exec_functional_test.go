package verbs

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestFunctionalEnvironmentSchemaIdentity(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, dsn := startBackupPG(ctx, t)
	e := ExecEnvironment{DSN: dsn}
	if ok, err := e.Schema(ctx); ok || err != nil {
		t.Fatal(ok, err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.Schema(ctx); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err = conn.Exec(ctx, "UPDATE sprint.meta SET hash='different'"); err != nil {
		t.Fatal(err)
	}
	// another build's store is a skew refusal naming both hashes, never
	// "not set up" (L6.6)
	if ok, err := e.Schema(ctx); ok || err == nil || !strings.Contains(err.Error(), "the store is at schema different (an unknown round), this build wants ") || !strings.Contains(err.Error(), "run the coordinator's build") {
		t.Fatal(ok, err)
	}
}
