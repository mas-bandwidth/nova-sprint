package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The two rows schema.sql seeds and a data-only dump of schema sprint
// carries (pg_dump --data-only --schema=sprint writes a COPY for every table
// in the schema, meta's one row with its hash and epoch's one row among
// them): RestoreBegin leaves them out so the dump's own load them.
const (
	seedMeta  = "INSERT INTO sprint.meta (id, schema) VALUES (1, 2) ON CONFLICT (id) DO NOTHING;"
	seedEpoch = "INSERT INTO sprint.epoch (id, n) VALUES (1, 1) ON CONFLICT (id) DO NOTHING;"
)

// restoreDDL is schema.sql with no CREATE SCHEMA and no seed rows: every
// table, function, trigger, index and CHECK, and nothing in any table.
var restoreDDL = strings.NewReplacer(seedMeta, "", seedEpoch, "").Replace(schemaObjects)

// RestoreBegin prepares tx, a transaction the caller owns on the connection
// that will load a data-only dump of schema sprint, for that load. It takes
// Migrate's advisory lock for the transaction (a no-op when the caller holds
// it), refuses a foreign schema sprint and one whose tables hold any row,
// creates the schema when it is absent, and runs schema.sql's DDL without the
// seed rows (the dump carries them). It disables the cards_fixed trigger for
// the load: the dump's COPY of sprint.cards is in the table's physical order,
// where an updated producer can follow its consumers, and the trigger checks a
// consumer's producer row by row. RestoreEnd enables it again and re-checks
// every card with it, in the same transaction, so no other session ever sees
// it disabled (ALTER TABLE holds an ACCESS EXCLUSIVE lock until the end).
func RestoreBegin(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLock); err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	present, meta, used, _, err := namespace(ctx, tx)
	switch {
	case err != nil:
		return fmt.Errorf("store: restore: %w", err)
	case used && !meta:
		return fmt.Errorf("store: restore: %w", errForeign)
	case present:
		if err := refuseRows(ctx, tx); err != nil {
			return err
		}
	default:
		if _, err := tx.Exec(ctx, createSchema); err != nil {
			return fmt.Errorf("store: restore: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, restoreDDL); err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	if _, err := tx.Exec(ctx, "ALTER TABLE sprint.cards DISABLE TRIGGER cards_fixed"); err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	return nil
}

// refuseRows refuses a schema sprint with a row in any of its tables.
func refuseRows(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'sprint' AND c.relkind = 'r' ORDER BY c.relname COLLATE "C"`)
	if err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	for _, table := range tables {
		var full bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM sprint."+pgx.Identifier{table}.Sanitize()+")").Scan(&full); err != nil {
			return fmt.Errorf("store: restore: %w", err)
		}
		if full {
			return fmt.Errorf("store: restore: sprint.%s has rows; a restore loads only into an empty store: use a new database, or drop schema sprint", table)
		}
	}
	return nil
}

// RestoreEnd checks the dump the caller loaded into tx after RestoreBegin,
// in the same transaction, and returns the store's identity; on an error the
// caller rolls back. It enables cards_fixed and runs it over every card (an
// UPDATE that changes nothing, so the trigger's own text is the check), then
// requires sprint.meta at this binary's version and hash, the epoch row, and
// the whole history to hold together (ValidateHistory, round 21).
func RestoreEnd(ctx context.Context, tx pgx.Tx) (SchemaIdentity, error) {
	for _, q := range []string{"ALTER TABLE sprint.cards ENABLE TRIGGER cards_fixed", "UPDATE sprint.cards SET id = id"} {
		if _, err := tx.Exec(ctx, q); err != nil {
			return SchemaIdentity{Expected: schemaHash}, fmt.Errorf("store: restore: the dump's cards break a rule of the schema: %w", err)
		}
	}
	version, hash, found, err := readMeta(ctx, tx)
	switch {
	case err != nil:
		return SchemaIdentity{Expected: schemaHash}, fmt.Errorf("store: restore: %w", err)
	case !found:
		return SchemaIdentity{Expected: schemaHash}, errors.New("store: restore: the dump has no sprint.meta row: it is not a data-only dump of a store")
	case version != SchemaVersion || hash != schemaHash:
		return SchemaIdentity{Expected: schemaHash, Hash: hash}, fmt.Errorf("store: restore: the dump is of schema version %d with hash %s, this binary's is version %d with hash %s: restore it with the binary that made it, then Migrate", version, hash, SchemaVersion, schemaHash)
	}
	var n int
	if err := tx.QueryRow(ctx, "SELECT n FROM sprint.epoch WHERE id = 1").Scan(&n); errors.Is(err, pgx.ErrNoRows) {
		return SchemaIdentity{Expected: schemaHash, Hash: hash}, errors.New("store: restore: the dump has no sprint.epoch row: it is not a whole data-only dump of a store")
	} else if err != nil {
		return SchemaIdentity{Expected: schemaHash, Hash: hash}, fmt.Errorf("store: restore: %w", err)
	}
	if err := validateHistory(ctx, tx); err != nil {
		return SchemaIdentity{Expected: schemaHash, Hash: hash}, fmt.Errorf("store: restore: the dump does not hold together: %w", err)
	}
	return identify(ctx, tx)
}

// checkRebuild is the store's log-equals-state check: Rebuild over events,
// every one from the first, is snap. The error names each part that differs.
func checkRebuild(snap Snapshot, events []Event) error {
	got := Rebuild(events)
	var differ []string
	for _, part := range []struct {
		name string
		a, b any
	}{
		{"seq", snap.Seq, got.Seq}, {"epoch", snap.Epoch, got.Epoch}, {"policy", snap.Policy, got.Policy},
		{"streams", snap.Streams, got.Streams}, {"cards", snap.Cards, got.Cards}, {"rows", snap.Rows, got.Rows},
		{"judgements", snap.Judgements, got.Judgements}, {"cursors", snap.Cursors, got.Cursors},
	} {
		if !reflect.DeepEqual(part.a, part.b) {
			differ = append(differ, part.name)
		}
	}
	if len(differ) > 0 {
		return fmt.Errorf("rebuilding the %d events gives a state that differs from the snapshot in %s", len(events), strings.Join(differ, ", "))
	}
	return nil
}
