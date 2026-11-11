package store

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is the version of schema.sql, kept in sprint.meta with the
// schema's hash. Version 2 adds to version 1: the card columns kind (with
// sentinel), of, after, author, base_sha, prior_head, result (with fault),
// bounces, bounce_base, cost, priced, priority, tier, paths, done_when, prose, review, finding and red; the card
// state done (round 29 dropped blocked); the row columns gen, held and reason (round 22
// dropped kind, width, tiers, model and harness: configuration, the
// roster's); the tables judgements and cursors; the events' policy; the meta
// hash; the triggers that keep a card's kind, of, stream, holder, tier, id
// and epoch, a row's, stream's or judgement's key and epoch, a policy version's
// columns but active, and the log (TRUNCATE included)
// fixed; and the CHECKs that keep every count at 0 or more. There is no
// upgrade path from version 1: a store at version 1 is refused like any
// other version. Within version 2 there are three: a store made by round 16's
// schema (round16Hash) or by round 17's, the one every round to 21 carried
// (round21Hash), or by round 22's (round22Hash), or by round 23's
// (round23Hash), is upgraded by Migrate (round 17's red column, round 22's
// dropped row columns, round 23's model column and row heartbeat columns,
// round 24's commit identity columns).
const SchemaVersion = 2

// round16Hash is the schema hash of round 16's schema.sql
// (testdata/schema-round16.sql, 36c1bbd6), an earlier build of version 2
// Migrate upgrades: running schema.sql on it adds cards.red (false on every
// card, with its CHECK), drops round 22's five row columns and the
// cards_live index, and changes nothing else, then the new hash is
// recorded. Any hash but these two and schemaHash is still refused.
const round16Hash = "58b09b225469ae56094b2eeafc8631c5e5cc9ae3dc5e1d2894a0f0434187373e"

// round21Hash is the schema hash of round 17's schema.sql, which every
// round to 21 carried (testdata/schema-round21.sql): running schema.sql on
// it drops sprint.rows' kind, width, tiers, model and harness (ALTER TABLE
// DROP COLUMN, round 22) and the cards_live index, in Migrate's one
// transaction, then the new hash is recorded.
const round21Hash = "cbe47774a69cf22749b6d414e1bc5689104820199313088df41975ef1771d491"

// round22Hash is the schema hash of round 22's schema.sql
// (testdata/schema-round22.sql): running schema.sql on it adds cards.model
// (” on every card, with its CHECK; round 23, the model a deal names) and the
// row heartbeat's four columns of sprint.rows, in Migrate's one transaction,
// then the new hash is recorded.
const round22Hash = "eb754483470ba67cfe6aa6c36646c19b4503a2fb10fd0344b3aa2fdff6a15b7e"

// round23Hash is the schema hash of round 23's schema.sql
// (testdata/schema-round23.sql): running schema.sql on it adds the epoch
// row's commit_name and commit_email (empty on the one row: no identity set;
// round 24, the identity every commit of the sprint is made as), in
// Migrate's one transaction, then the new hash is recorded.
const round23Hash = "e4d87e423396c701797836720e749b0d8ea81f8879cf2811f89275970a684b01"

// round24Hash is the schema hash of v2-stitch's round 24 schema.sql
// (testdata/schema-round24.sql: model, the heartbeat and the commit
// identity, no usage and no balances): running schema.sql on it adds
// cards.usage and sprint.balances (round 25), in Migrate's one transaction,
// then the new hash is recorded.
const round24Hash = "e7561ffcff21033ff3b0a7dd615e32b6c9f0c33278278fe30e1912fec5e57aca"

// round25Hash is the schema hash of round 25's schema.sql
// (testdata/schema-round25.sql), the deployed line's: round 22's schema
// plus cards.usage and sprint.balances, the live store's on 2026-11-07.
// Running schema.sql on it adds cards.model, the row heartbeat's columns
// (round 23) and the epoch row's commit identity (round 26), in Migrate's
// one transaction, then the new hash is recorded.
const round25Hash = "9c9be7f5f7ae333043ff667a93f473d1bdc6c071bfad043f1e8d9ca1f4433d0e"

// round26Hash is the schema hash of round 26's schema.sql
// (testdata/schema-round26.sql): running schema.sql on it adds the row
// heartbeat's cpu_peak_deci (round 27, the CPU busy peak of the last 10
// seconds), in Migrate's one transaction, then the new hash is recorded.
const round26Hash = "475ac9c67bf8c1d041e7a54b69f7c4e3a0b0d56bfd43a11dc6e4db31ff60dfed"

// round27Hash is the schema hash of round 27's schema.sql
// (testdata/schema-round27.sql): running schema.sql on it adds the row
// heartbeat's version (round 28, the release of the binary that beat;
// defect D3), in Migrate's one transaction, then the new hash is recorded.
const round27Hash = "5aae26316b0ddbffbe2668a180446cd027dbf237a8e05baa85038786de6858b5"

// round28Hash is the schema hash of round 28's schema.sql
// (testdata/schema-round28.sql): its cards admit the state blocked. Running
// schema.sql on it replaces the state's CHECK without blocked (round 29;
// Glenn, 2026-11-07: blocked is no state), and Migrate moves every blocked
// card to waiting with its reason (migrateBlocked), in its one transaction,
// then the new hash is recorded.
const round28Hash = "45e961bd30f7a3497cc42ffa132adca022ee48703a9b218369e6c7fbae79a24e"

// round29Hash is the schema hash of round 29's schema.sql
// (testdata/schema-round29.sql): its cards have no bounce_base. Running
// schema.sql on it adds cards.bounce_base, 0 on every card (round 30: an
// unhold gives a producer a fresh bounce budget), then the new hash is
// recorded.
const round29Hash = "1a911dc3cf8cac36666a06784e9ddb54ebeeb91a12ee2b3123b6ad556a7c02d7"

// round30Hash is the schema hash of round 30's schema.sql
// (testdata/schema-round30.sql): its streams have no gates. Running
// schema.sql on it adds streams.gates, none on every stream (round 31: a
// stream's gates), then the new hash is recorded.
const round30Hash = "c5bc4eca91743d1afeb6e00ab61db4247910ef04d887235c124c701f2a5d7f07"

// round31Hash is the schema hash of round 31's schema.sql
// (testdata/schema-round31.sql): its policy versions have no index on
// name. Running schema.sql on it adds policy_name (round 32: a version
// read by its name), then the new hash is recorded.
const round31Hash = "c66a2c0674ff709923dcca09806a832982353c50726adbf85e7ab4305db1246c"

// schemaHash is the sha256, in hex, of the schema the binary carries with
// comments, indentation and blank lines stripped, so editing a comment does
// not refuse every store.
var schemaHash = hashSchema(schemaSQL)

// createSchema is schema.sql's one statement that needs CREATE on the
// database: PostgreSQL checks that privilege before IF NOT EXISTS, so a role
// that owns an existing schema sprint but may not create schemas (the
// least-privilege setup) is refused it. Migrate runs it only when the schema
// is absent and otherwise runs schemaObjects, the rest of schema.sql. The
// hash is still of the whole file.
const createSchema = "CREATE SCHEMA IF NOT EXISTS sprint;"

var schemaObjects = strings.Replace(schemaSQL, createSchema, "", 1)

// hashSchema strips a -- comment only outside a single-quoted string literal
// and a double-quoted identifier (a doubled quote closes and reopens, so it
// stays inside), across lines, so a '--' in a literal is hashed. Dollar
// quotes ($$ ... $$) are scanned as code, since the schema's are all function
// bodies: their comments are stripped and their literals kept, so a -- in a
// $$ string literal would be stripped (the schema has none). E-prefixed
// escape strings are not read (the schema has none). Two more limits, both
// absent from the schema and asserted by the tests: a quote inside a /* */
// comment opens a literal, and indentation inside a literal that spans lines
// is stripped.
func hashSchema(sql string) string {
	var b strings.Builder
	var quote byte // the open quote: ' a literal, " an identifier, 0 none
	for line := range strings.Lines(sql) {
		cut := len(line)
		for i := 0; i < cut; i++ {
			switch ch := line[i]; {
			case quote == 0 && (ch == '\'' || ch == '"'):
				quote = ch
			case ch == quote:
				quote = 0
			case quote == 0 && strings.HasPrefix(line[i:], "--"):
				cut = i
			}
		}
		if line = strings.TrimSpace(line[:cut]); line != "" {
			b.WriteString(line + "\n")
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

// migrateLock is the advisory lock key Migrate holds for its transaction, so
// concurrent Migrates of one store run one at a time.
const migrateLock = 0x6e6f7661 // "nova"

// PG is the Store in PostgreSQL, schema sprint.
type PG struct {
	pool *pgxpool.Pool
	// fault is a fault-injection seam for the functional tests (kept, not
	// dead code). It is called with "locked" inside a write's transaction
	// once the writer lock is held, and with "committed" after COMMIT and
	// before the receipt is returned. An error at committed is ErrUnknown (a
	// reply lost after the commit, guarantee 3), one at locked is known like
	// any other before COMMIT; a panic in it proves the lock is released. Nil
	// outside tests.
	fault func(stage string) error
}

// dsnUserinfo and dsnPassword find a password a DSN's parse error may quote.
var (
	dsnUserinfo = regexp.MustCompile(`://[^/@\s]*@`)
	dsnPassword = regexp.MustCompile(`(?i)\bpassword\s*=\s*\S+`)
)

// dsnParseError is what with the DSN parse's own reason, any password it
// quotes removed (pgx redacts its own; this also covers a quoted URL); err
// is kept for errors.Is and errors.As.
func dsnParseError(what string, err error) error {
	text := dsnUserinfo.ReplaceAllString(err.Error(), "://<redacted>@")
	text = dsnPassword.ReplaceAllString(text, "password=<redacted>")
	return &parseError{text: what + ": " + text, err: err}
}

type parseError struct {
	text string
	err  error
}

func (e *parseError) Error() string { return e.text }
func (e *parseError) Unwrap() error { return e.err }

// IdleTxTimeout is how long PostgreSQL lets one of the sprint's sessions sit
// idle in a transaction before it ends the session, and with it the
// transaction: a client that stalls inside one (a stopped process, a lost
// network) otherwise keeps its locks and snapshot until TCP gives up, hours,
// and every DDL and, queued behind it, every reader waits. A sprint
// transaction runs its statements milliseconds apart, so a minute is far past
// any healthy pause and bounds what a stall can hold (PR #658).
const IdleTxTimeout = "60s"

// BoundIdleTx sets idle_in_transaction_session_timeout to IdleTxTimeout on
// every connection made with cfg, unless the dsn already sets it.
func BoundIdleTx(cfg *pgx.ConnConfig) {
	if _, set := cfg.RuntimeParams["idle_in_transaction_session_timeout"]; set {
		return
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["idle_in_transaction_session_timeout"] = IdleTxTimeout
}

// Open connects to the store at dsn (NOVA_PG_DSN, no password in it) with
// password (NOVA_PG_PASSWORD, may be empty). It never quotes the dsn back.
func Open(ctx context.Context, dsn, password string) (*PG, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, dsnParseError("store: the postgres dsn could not be parsed", err)
	}
	if password != "" {
		cfg.ConnConfig.Password = password
	}
	BoundIdleTx(cfg.ConnConfig)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: cannot reach postgres at %s:%d: %w", cfg.ConnConfig.Host, cfg.ConnConfig.Port, err)
	}
	return &PG{pool: pool}, nil
}

// Close closes the connection pool.
func (p *PG) Close() { p.pool.Close() }

// Migrate applies schema.sql to an empty store or one at SchemaVersion made
// by this build of it, and refuses a store at any other version, or at this
// version with another schema hash (sprint.meta), naming both, and a schema
// sprint that holds anything but no sprint.meta (not made by nova-sprint):
// any object of any kind in the namespace, which PostgreSQL records as a
// pg_depend row on it (a relation, type, function, operator, collation, text
// search object, operator family, ...). The other hashes it takes are
// round16Hash, round21Hash, round22Hash and round23Hash: schema.sql adds cards.red to a round 16
// store, drops the five configuration columns of sprint.rows from either of the first two
// (round 22), adds cards.model and the row heartbeat's columns to the first three (round 23)
// and the epoch row's commit identity to all four (round 24), and the new hash is recorded, in the same transaction. When schema sprint already exists it
// skips CREATE SCHEMA (createSchema), so the schema's owner needs no CREATE
// on the database (round 19). An upgrade also logs the store's stop when its log does
// not rebuild to it (logStop). Twice is a no-op, and concurrent Migrates are safe: each holds one
// advisory lock.
func (p *PG) Migrate(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // ignored: a no-op after the commit
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLock); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	present, meta, used, _, err := namespace(ctx, tx) // the foreign check CheckSchema shares (schema.go)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if used && !meta {
		return fmt.Errorf("store: migrate: %w", errForeign)
	}
	upgrade := false
	if meta {
		v, hash, _, err := readMeta(ctx, tx) // no row is version 0, refused below
		if err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
		if err := metaRefusal(v, hash); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
		upgrade = hash != schemaHash
	}
	sql := schemaSQL
	if present {
		sql = schemaObjects // no CREATE SCHEMA: the owner may lack CREATE on the database
	}
	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if err := migrateBlocked(ctx, tx); err != nil {
		return err
	}
	if err := migrateLanded(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE sprint.meta SET hash = $1 WHERE id = 1", schemaHash); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if err := freshStop(ctx, tx, meta); err != nil {
		return err
	}
	if upgrade {
		if err := logStop(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// migrateBlocked moves every card a store made before round 29 left in
// state blocked to waiting, held (Glenn, 2026-11-07: "waiting is where stuff
// goes where it's having problems"), in Migrate's transaction, then
// validates the state's CHECK, which schema.sql adds NOT VALID. A card was
// blocked to keep it out of the deal, so its reason becomes one that holds
// (HoldingReason): the text kept, a bounce limit's "bounced N times, the
// limit is M" prefixed "held: ". The current epoch's cards move through one
// op of the protocol (actor store-migrate), so their gens rise and the log
// rebuilds to them (guarantee 5); an earlier epoch's, which no rebuild
// reads, by plain UPDATEs. A store with none logs nothing, so a second
// Migrate is a no-op.
func migrateBlocked(ctx context.Context, tx pgx.Tx) error {
	var epoch int
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT n, transaction_timestamp() FROM sprint.epoch WHERE id = 1").Scan(&epoch, &now); err != nil {
		return fmt.Errorf("store: migrate: reading the epoch: %w", err)
	}
	type blocked struct {
		ID, Reason string
		Epoch      int
	}
	rows, err := tx.Query(ctx, "SELECT id, reason, epoch FROM sprint.cards WHERE state = 'blocked' ORDER BY epoch, id")
	if err != nil {
		return fmt.Errorf("store: migrate: reading the blocked cards: %w", err)
	}
	cards, err := pgx.CollectRows(rows, pgx.RowToStructByPos[blocked])
	if err != nil {
		return fmt.Errorf("store: migrate: reading the blocked cards: %w", err)
	}
	var writes []Write
	for _, c := range cards {
		reason := HoldingReason(c.Reason)
		if c.Epoch == epoch {
			writes = append(writes, SetCardState{Card: c.ID, State: "waiting"})
			if reason != c.Reason {
				writes = append(writes, SetCardReason{Card: c.ID, Reason: reason})
			}
			continue
		}
		if _, err := tx.Exec(ctx, "UPDATE sprint.cards SET state = 'waiting', reason = $1 WHERE id = $2 AND epoch = $3", reason, c.ID, c.Epoch); err != nil {
			return fmt.Errorf("store: migrate: moving an earlier epoch's blocked card %s to waiting: %w", c.ID, err)
		}
	}
	reopen, err := reopenHeld(ctx, tx, epoch)
	if err != nil {
		return err
	}
	writes = append(writes, reopen...)
	if len(writes) > 0 {
		op := Op{ID: "migrate-blocked-" + schemaHash, Epoch: epoch, Actor: "store-migrate", Kind: "migrate", At: at(now), Writes: writes}
		r, err := (&PG{}).inTx(ctx, tx, op, check, func(t txn, epoch int) (string, *State, *State, error) { return run(t, epoch, op) })
		switch {
		case err != nil:
			return fmt.Errorf("store: migrate: moving the blocked cards to waiting: %w", err)
		case !r.Accepted:
			return fmt.Errorf("store: migrate: moving the blocked cards to waiting was refused: %s", r.Reason)
		}
	}
	if _, err := tx.Exec(ctx, "ALTER TABLE sprint.cards VALIDATE CONSTRAINT cards_state_check"); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// migrateLanded moves every producer of the current epoch recorded landed
// before landed meant on main (Glenn, 2026-11-08: "to me landed means on
// main"; its reason does not start with LandedReason, the v1 import's
// excluded) back to merging, on base at the merge its reason names
// ("landed at <merge>"; none named: on base with none, and the promotion
// checks its head), through one op of the protocol (actor store-migrate),
// so the log rebuilds to it. The store does not know main: the landing's
// promotion lands again, at once, each one whose merge is on main
// (LANDING.md, "Landed means on main"). Earlier epochs, which no rebuild
// reads, are left as they are. A second Migrate finds none and logs
// nothing.
func migrateLanded(ctx context.Context, tx pgx.Tx) error {
	type landed struct{ ID, Reason, Base string }
	rows, err := tx.Query(ctx, `SELECT c.id, c.reason, s.base FROM sprint.cards c
		JOIN sprint.streams s ON s.epoch = c.epoch AND s.name = c.stream
		JOIN sprint.epoch e ON e.id = 1 AND e.n = c.epoch
		WHERE c.kind = 'producer' AND c.state = 'landed' AND c.reason NOT LIKE $1 AND c.reason NOT LIKE $2
		ORDER BY c.id`, LandedReason+"%", ImportedReason+"%")
	if err != nil {
		return fmt.Errorf("store: migrate: reading the producers recorded landed off main: %w", err)
	}
	cards, err := pgx.CollectRows(rows, pgx.RowToStructByPos[landed])
	if err != nil {
		return fmt.Errorf("store: migrate: reading the producers recorded landed off main: %w", err)
	}
	if len(cards) == 0 {
		return nil
	}
	var epoch int
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT n, transaction_timestamp() FROM sprint.epoch WHERE id = 1").Scan(&epoch, &now); err != nil {
		return fmt.Errorf("store: migrate: reading the epoch: %w", err)
	}
	var writes []Write
	sum := sha256.New()
	for _, c := range cards {
		fmt.Fprintf(sum, "%s\x00%s\x00", c.ID, c.Reason)
		merge := ""
		if rest, ok := strings.CutPrefix(c.Reason, "landed at "); ok {
			merge, _, _ = strings.Cut(rest, ":")
		}
		writes = append(writes, SetCardState{Card: c.ID, State: "merging"},
			SetCardReason{Card: c.ID, Reason: OnBaseReason(c.Base, merge) + " (" + c.Reason + "); " + MigratedNote})
	}
	op := Op{ID: fmt.Sprintf("migrate-landed-%x", sum.Sum(nil)[:16]), Epoch: epoch, Actor: "store-migrate", Kind: "migrate", At: at(now), Writes: writes}
	r, err := (&PG{}).inTx(ctx, tx, op, check, func(t txn, epoch int) (string, *State, *State, error) { return run(t, epoch, op) })
	switch {
	case err != nil:
		return fmt.Errorf("store: migrate: moving the producers recorded landed off main to merging: %w", err)
	case !r.Accepted:
		return fmt.Errorf("store: migrate: moving the producers recorded landed off main to merging was refused: %s", r.Reason)
	}
	return nil
}

// reopenHeld is the writes that move the current epoch's open judgements of
// the kind round 29 dropped to kind held: each is expired, naming its
// successor, and the successor opened with its question, answered by unhold
// or drop, no deadline (a held card waits for the coordinator). A
// judgement's id is fixed (sprint.key_fixed), so the kind in it is changed
// by a new judgement, never an UPDATE.
func reopenHeld(ctx context.Context, tx pgx.Tx, epoch int) ([]Write, error) {
	type open struct{ ID, About, Question string }
	rows, err := tx.Query(ctx, "SELECT id, about, question FROM sprint.judgements WHERE kind = 'blocked' AND state = 'open' AND epoch = $1 ORDER BY id", epoch)
	if err != nil {
		return nil, fmt.Errorf("store: migrate: reading the open judgements to reopen as held: %w", err)
	}
	js, err := pgx.CollectRows(rows, pgx.RowToStructByPos[open])
	if err != nil {
		return nil, fmt.Errorf("store: migrate: reading the open judgements to reopen as held: %w", err)
	}
	var writes []Write
	for _, j := range js {
		n := 1
		for ; ; n++ {
			var taken bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM sprint.judgements WHERE id = $1 AND epoch = $2)", fmt.Sprintf("held.%s.%d", j.About, n), epoch).Scan(&taken); err != nil {
				return nil, fmt.Errorf("store: migrate: %w", err)
			}
			if !taken {
				break
			}
		}
		id := fmt.Sprintf("held.%s.%d", j.About, n)
		writes = append(writes, ExpireJudgement{ID: j.ID, Reason: "round 29: reopened as " + id},
			PutJudgement{ID: id, Kind: "held", About: j.About, Question: j.Question, Group: "held",
				Command: "nova-sprint judge " + id + " <choice> --reason <why>   choices: unhold | drop"})
	}
	return writes, nil
}

// HoldingReason is the reason round 29 gives a card it moves out of the
// dropped blocked state: reason itself when it already holds (a hold's
// "held: <why>", not a stream park's "held: stream <s> is held", or a
// missing-route fault's "missing-route: ..."), else "held: " and reason, so
// the card stays out of the deal. It mirrors card.HeldReason, which this
// layer cannot import; card's TestMigratedReasonHolds binds the two.
func HoldingReason(reason string) string {
	if strings.HasPrefix(strings.TrimSpace(reason), "missing-route:") {
		return reason
	}
	if strings.HasPrefix(reason, "held: ") {
		name, park := strings.CutPrefix(reason, "held: stream ")
		if !park || !strings.HasSuffix(name, " is held") || strings.Contains(strings.TrimSuffix(name, " is held"), " ") {
			return reason
		}
	}
	if reason == "" {
		return "held: no reason was recorded"
	}
	return "held: " + reason
}

// freshStop stops a store Migrate has just made (there was no sprint.meta
// before it), with FreshStop's reason, in Migrate's transaction: a fresh
// store moves nothing until the coordinator's first start. The column's
// default stays false, so the schema hash is unchanged and a store made
// before this keeps its own stop.
func freshStop(ctx context.Context, tx pgx.Tx, existed bool) error {
	if existed {
		return nil
	}
	if _, err := tx.Exec(ctx, "UPDATE sprint.epoch SET stopped = $1, stop_reason = $2 WHERE id = 1", FreshStop.Stopped, FreshStop.Reason); err != nil {
		return fmt.Errorf("store: migrate: the fresh store's stop (%s) was not written: %w", FreshStop.Reason, err)
	}
	return nil
}

// logStop makes an upgraded store's log rebuild to its stop (guarantee 5),
// in Migrate's transaction. A store made before FreshStop began running
// with nothing logged, and a rebuild starts never started, so a store
// whose log does not end at the stop it has (the last logged stop, else
// FreshStop) gets SetStopped events from the log's stop to its own, through
// the protocol: the epoch row is set back to the log's stop first, so each
// before-image is what the log says. One event when the flag differs; two
// (through the other flag) when only the reason does, since SetStopped
// refuses an unchanged flag. A store whose log agrees logs nothing, so a
// second Migrate is a no-op.
func logStop(ctx context.Context, tx pgx.Tx) error {
	var have Stop
	var epoch int
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT stopped, stop_reason, n, transaction_timestamp() FROM sprint.epoch WHERE id = 1").Scan(&have.Stopped, &have.Reason, &epoch, &now); err != nil {
		return fmt.Errorf("store: migrate: reading the stop: %w", err)
	}
	logged := FreshStop
	var image []byte
	err := tx.QueryRow(ctx, "SELECT after->'stop' FROM sprint.events WHERE accepted AND after ? 'stop' ORDER BY seq DESC LIMIT 1").Scan(&image)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("store: migrate: reading the logged stop: %w", err)
	default:
		if err = json.Unmarshal(image, &logged); err != nil {
			return fmt.Errorf("store: migrate: the logged stop %s does not decode: %w", image, err)
		}
	}
	if logged == have {
		return nil
	}
	if _, err = tx.Exec(ctx, "UPDATE sprint.epoch SET stopped = $1, stop_reason = $2 WHERE id = 1", logged.Stopped, logged.Reason); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	steps := []Stop{have}
	if logged.Stopped == have.Stopped {
		steps = []Stop{{Stopped: !have.Stopped, Reason: "migrate"}, have}
	}
	for i, st := range steps {
		op := Op{ID: fmt.Sprintf("migrate-stop-%d-%s", i+1, schemaHash), Epoch: epoch, Actor: "store-migrate", Kind: "stop", At: at(now), Writes: []Write{SetStopped(st)}}
		r, err := (&PG{}).inTx(ctx, tx, op, check, func(t txn, epoch int) (string, *State, *State, error) { return run(t, epoch, op) })
		switch {
		case err != nil:
			return fmt.Errorf("store: migrate: logging the store's stop: %w", err)
		case !r.Accepted:
			return fmt.Errorf("store: migrate: logging the store's stop was refused: %s", r.Reason)
		}
	}
	return nil
}

// known is a failure inside the transaction, before COMMIT, whose outcome
// is known, as a plain error: an invalid body, an error the server returned
// (any SQLSTATE), or one the client raised (a scan, an encode, a receipt that
// does not decode, a counter at the top of its column). The transaction
// rolls back, so nothing landed. It is nil, the outcome unknown, when ctx is
// done, or when the connection was lost after a request may have reached the
// server (pgconn.SafeToRetry says when none did, and that is known).
func known(ctx context.Context, closed bool, op Op, err error) error {
	var pe *pgconn.PgError
	isPG := errors.As(err, &pe)
	switch {
	case errors.Is(err, ErrInvalid):
		return err
	case ctx.Err() != nil:
		return nil
	case pgconn.SafeToRetry(err):
		// Known: the request never reached the server, so nothing can have
		// landed. Leave the switch for the plain error below, as for any
		// error before COMMIT, ahead of the lost-connection case.
	case closed || isPG && lost(pe):
		return nil
	}
	if !isPG {
		return fmt.Errorf("store: op %s failed before COMMIT, so nothing landed: %w", op.ID, err)
	}
	on := ""
	switch {
	case pe.ConstraintName != "":
		on = " on constraint " + pe.ConstraintName
	case pe.ColumnName != "":
		on = " on column " + pe.ColumnName
	case pe.TableName != "":
		on = " on table " + pe.TableName
	}
	return fmt.Errorf("store: op %s: postgres refused it before COMMIT, so nothing landed: SQLSTATE %s%s: %s: %w", op.ID, pe.Code, on, pe.Message, err)
}

// lost is a server error that ends the session rather than answering the
// statement (severity FATAL or PANIC, or a connection exception, class 08):
// a connection error, so the outcome is unknown.
func lost(pe *pgconn.PgError) bool {
	return pe.Severity == "FATAL" || pe.Severity == "PANIC" || strings.HasPrefix(pe.Code, "08")
}

// write runs protocol in one READ COMMITTED transaction holding the writer lock,
// so writers run one at a time and nothing needs a retry. A failure after
// BEGIN and before COMMIT is a plain error unless the connection or ctx went
// (known, above); any error during or after COMMIT is ErrUnknown. A panic rolls the transaction
// back, releasing the lock, and is re-raised.
func (p *PG) write(ctx context.Context, op Op, valid func(Op) error, rule rules) (r Receipt, err error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Receipt{}, fmt.Errorf("store: begin: %w", err)
	}
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback(context.WithoutCancel(ctx)) // ignored: the error or panic that got us here is the one reported
		}
	}()
	if r, err = p.inTx(ctx, tx, op, valid, rule); err != nil {
		if k := known(ctx, tx.Conn().IsClosed(), op, err); k != nil {
			return Receipt{}, k
		}
		return Receipt{}, fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	done = true
	if err := tx.Commit(ctx); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	if err := p.injected("committed"); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	return r, nil
}

func (p *PG) inTx(ctx context.Context, tx pgx.Tx, op Op, valid func(Op) error, rule rules) (Receipt, error) {
	if _, err := tx.Exec(ctx, "LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return Receipt{}, err
	}
	if err := p.injected("locked"); err != nil {
		return Receipt{}, err
	}
	var epoch int
	if err := tx.QueryRow(ctx, "SELECT n FROM sprint.epoch WHERE id = 1").Scan(&epoch); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT writes"); err != nil { // discard rolls back to it
		return Receipt{}, err
	}
	return protocol(&pgTx{ctx: ctx, tx: tx, epoch: epoch}, epoch, op, valid, rule)
}

func (p *PG) injected(stage string) error {
	if p.fault == nil {
		return nil
	}
	return p.fault(stage)
}

// Apply runs op in one transaction (protocol, apply.go).
func (p *PG) Apply(ctx context.Context, op Op) (Receipt, error) { return applyOp(p.writer(ctx), op) }

// Clear opens a new epoch, carrying every row into it, in one transaction.
func (p *PG) Clear(ctx context.Context, op Op) (Receipt, error) { return clearOp(p.writer(ctx), op) }

func (p *PG) writer(ctx context.Context) writer {
	return func(op Op, valid func(Op) error, rule rules) (Receipt, error) { return p.write(ctx, op, valid, rule) }
}

func jsonArg(s *State) any {
	if s == nil {
		return nil
	}
	return string(encode(s))
}

// replaySQL is the stored receipt of an op id with its event's after-image,
// which the receipt does not repeat (readReceipt).
const replaySQL = "SELECT receipt, after FROM sprint.events WHERE op = $1"

func scanReplay(row pgx.Row) (Receipt, bool, error) {
	var b, after []byte
	err := row.Scan(&b, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, err
	}
	a, err := readState(after)
	if err != nil {
		return Receipt{}, false, err
	}
	r, err := readReceipt(b, a)
	return r, err == nil, err
}

// Replay returns the stored receipt of an op id, and false when it is
// unknown (an id no op could have, empty, over MaxName bytes or holding a NUL, is unknown).
func (p *PG) Replay(ctx context.Context, opID string) (Receipt, bool, error) {
	if checkID(Op{ID: opID}) != nil {
		return Receipt{}, false, nil
	}
	r, ok, err := scanReplay(p.pool.QueryRow(ctx, replaySQL, opID))
	if err != nil {
		return Receipt{}, false, fmt.Errorf("store: replay: %w", err)
	}
	return r, ok, nil
}

// Policy returns one policy version; ErrNotFound when there is none.
func (p *PG) Policy(ctx context.Context, version int) (PolicyVersion, error) {
	if version < 0 || version > math.MaxInt32 {
		return PolicyVersion{}, fmt.Errorf("%w: policy version %d", ErrNotFound, version)
	}
	v, ok, err := one(p.pool.QueryRow(ctx, "SELECT "+policyCols+" FROM sprint.policy WHERE version = $1", version), scanPolicy)
	switch {
	case err != nil:
		return v, fmt.Errorf("store: policy: %w", err)
	case !ok:
		return v, fmt.Errorf("%w: policy version %d", ErrNotFound, version)
	}
	return v, nil
}

// policiesNamedSQL is PoliciesNamed's one read, by policy_name.
const policiesNamedSQL = "SELECT " + policyCols + " FROM sprint.policy WHERE name = $1 AND kind = $2 ORDER BY version DESC LIMIT $3"

// PoliciesNamed reads the newest max versions of kind under name, newest
// first, by policy_name (name, version).
func (p *PG) PoliciesNamed(ctx context.Context, kind, name string, max int) ([]PolicyVersion, error) {
	rows, err := p.pool.Query(ctx, policiesNamedSQL, name, kind, max)
	if err != nil {
		return nil, fmt.Errorf("store: policies named: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PolicyVersion, error) { return scanPolicy(r) })
	if err != nil {
		return nil, fmt.Errorf("store: policies named: %w", err)
	}
	return out, nil
}
