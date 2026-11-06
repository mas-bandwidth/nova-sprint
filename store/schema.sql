-- The sprint store: layer 1 of nova-sprint v2 (docs/LAYER-1-STORE.md).
-- Idempotent: applying it twice is a no-op. Migrate checks sprint.meta before
-- it runs this, and refuses a store at another schema version. Nothing is
-- ever deleted; a clear increments sprint.epoch and every stream, card, row
-- and judgement carries the epoch it was written in (a cursor is global, as
-- seq is). Every count is 0 or more by a CHECK. Version 2 is the
-- version with the two card kinds (kind with sentinel, of, after, author,
-- base_sha, result, the states blocked and done; prior_head, bounces, cost,
-- priced, priority, the result fault; tier, paths, done_when, prose), row
-- gen, held, tiers, model and harness, the judgements and cursors tables and
-- the events' policy; there is no upgrade path from version 1.

CREATE SCHEMA IF NOT EXISTS sprint;

-- hash is the sha256 of this file as the binary that made the store carries
-- it, comments and blank lines stripped: Migrate sets it on a new store and
-- refuses a store whose hash differs at the same version (a different build
-- of the schema).
CREATE TABLE IF NOT EXISTS sprint.meta (
    id     integer PRIMARY KEY CHECK (id = 1),
    schema integer NOT NULL,
    hash   text    NOT NULL DEFAULT ''
);
INSERT INTO sprint.meta (id, schema) VALUES (1, 2) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS sprint.epoch (
    id integer PRIMARY KEY CHECK (id = 1),
    n  integer NOT NULL CHECK (n >= 1)
);
INSERT INTO sprint.epoch (id, n) VALUES (1, 1) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS sprint.streams (
    name  text    NOT NULL,
    repo  text    NOT NULL DEFAULT '',
    base  text    NOT NULL DEFAULT '',
    held  boolean NOT NULL DEFAULT false,
    pos   integer NOT NULL DEFAULT 0 CHECK (pos >= 0),
    epoch integer NOT NULL,
    PRIMARY KEY (epoch, name)
);

CREATE TABLE IF NOT EXISTS sprint.rows (
    name    text    NOT NULL,
    kind    text    NOT NULL CHECK (kind IN ('worker', 'kin')),
    width   integer NOT NULL DEFAULT 1 CHECK (width >= 0),
    state   text    NOT NULL CHECK (state IN ('up', 'down')),
    session text    NOT NULL DEFAULT '',
    flaps   integer NOT NULL DEFAULT 0 CHECK (flaps >= 0),
    held    boolean NOT NULL DEFAULT false,
    gen     integer NOT NULL DEFAULT 0 CHECK (gen >= 0),
    tiers   text[]  NOT NULL DEFAULT '{}' CHECK (tiers <@ ARRAY['flash', 'pro', 'heavy', 'frontier']::text[]),
    model   text    NOT NULL DEFAULT '',
    harness text    NOT NULL DEFAULT '',
    epoch   integer NOT NULL,
    PRIMARY KEY (epoch, name)
);

-- One NOT NULL state column with a CHECK and one holder column with a
-- foreign key: a card is in exactly one state and has at most one holder, by
-- the schema. A producer's or sentinel's "of" is NULL (the empty string in Go,
-- as holder); neither is ever held, and a sentinel has no brief;
-- a consumer's names a card of the same epoch by a foreign key.
CREATE TABLE IF NOT EXISTS sprint.cards (
    id       text    NOT NULL,
    kind     text    NOT NULL CHECK (kind IN ('producer', 'sentinel', 'work', 'read')),
    "of"     text,
    stream   text    NOT NULL,
    state    text    NOT NULL CHECK (state IN ('waiting', 'ready', 'working', 'review', 'merging', 'landed', 'fail', 'blocked', 'done')),
    gen      integer NOT NULL DEFAULT 0 CHECK (gen >= 0),
    attempt  integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    score    integer NOT NULL DEFAULT 0 CHECK (score >= 0),
    needs    text[]  NOT NULL DEFAULT '{}',
    after    text[]  NOT NULL DEFAULT '{}',
    holder   text,
    author   text    NOT NULL DEFAULT '',
    branch   text    NOT NULL DEFAULT '',
    base_sha text    NOT NULL DEFAULT '',
    prior_head text  NOT NULL DEFAULT '',
    head     text    NOT NULL DEFAULT '',
    pr       integer NOT NULL DEFAULT 0 CHECK (pr >= 0),
    result   text    NOT NULL DEFAULT '' CHECK (result IN ('', 'ok', 'fix', 'fail', 'fault')),
    bounces  integer NOT NULL DEFAULT 0 CONSTRAINT cards_bounces_min_check CHECK (bounces >= 0),
    cost     bigint  NOT NULL DEFAULT 0 CHECK (cost >= 0),
    priced   boolean NOT NULL DEFAULT false,
    priority text    NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'critical', 'blocker')),
    brief    text    NOT NULL DEFAULT '',
    reason   text    NOT NULL DEFAULT '',
    tier     text    NOT NULL CHECK (tier IN ('flash', 'pro', 'heavy', 'frontier')),
    paths    text[]  NOT NULL DEFAULT '{}',
    done_when text[] NOT NULL DEFAULT '{}',
    prose    text    NOT NULL DEFAULT '',
    epoch    integer NOT NULL,
    PRIMARY KEY (epoch, id),
    CONSTRAINT cards_of_check CHECK ((kind IN ('producer', 'sentinel')) = ("of" IS NULL)),
    CONSTRAINT cards_needs_check CHECK (kind IN ('producer', 'sentinel') OR needs = '{}'),
    CONSTRAINT cards_after_check CHECK (kind IN ('producer', 'sentinel') OR after = '{}'),
    CONSTRAINT cards_brief_check CHECK (kind <> 'sentinel' OR brief = ''),
    CONSTRAINT cards_consumer_priority_check CHECK (kind IN ('producer', 'sentinel') OR priority = 'normal'),
    CONSTRAINT cards_holder_check CHECK ((kind IN ('work', 'read')) = (holder IS NOT NULL)),
    CONSTRAINT cards_prior_head_check CHECK (kind = 'work' OR prior_head = ''),
    CONSTRAINT cards_bounces_check CHECK (kind = 'producer' OR bounces = 0),
    CONSTRAINT cards_producer_cost_check CHECK (kind IN ('work', 'read') OR (cost = 0 AND NOT priced)),
    CONSTRAINT cards_producer_result_check CHECK (kind IN ('work', 'read') OR result = ''),
    FOREIGN KEY (epoch, stream) REFERENCES sprint.streams (epoch, name),
    FOREIGN KEY (epoch, holder) REFERENCES sprint.rows (epoch, name),
    FOREIGN KEY (epoch, "of") REFERENCES sprint.cards (epoch, id)
);

-- What a CHECK cannot say: a card's id, epoch, kind, of, stream, holder and
-- tier never change once it is written (there is no write for any of them;
-- Johnny's review 2026-11-06: a working card could drop its holder and keep
-- the slot), and a consumer's of names a producer of its own stream and tier
-- (the foreign key says only that it names a card). A consumer's attempt is fixed at add
-- too, but a trigger cannot tell the store's own writes from a caller's, so
-- that rule is the store's alone.
CREATE OR REPLACE FUNCTION sprint.cards_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.id IS DISTINCT FROM OLD.id OR NEW.epoch IS DISTINCT FROM OLD.epoch OR NEW.kind IS DISTINCT FROM OLD.kind
            OR NEW."of" IS DISTINCT FROM OLD."of" OR NEW.stream IS DISTINCT FROM OLD.stream OR NEW.holder IS DISTINCT FROM OLD.holder OR NEW.tier IS DISTINCT FROM OLD.tier) THEN
        RAISE EXCEPTION 'card % changes its id, epoch, kind, of, stream, holder or tier; they are fixed when the card is added', OLD.id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'cards_fixed_check';
    END IF;
    IF NEW."of" IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sprint.cards c WHERE c.epoch = NEW.epoch AND c.id = NEW."of" AND c.kind = 'producer') THEN
        RAISE EXCEPTION 'card % is a consumer of %, which is not a producer', NEW.id, NEW."of"
            USING ERRCODE = 'check_violation', CONSTRAINT = 'cards_of_producer_check';
    END IF;
    IF NEW."of" IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sprint.cards c WHERE c.epoch = NEW.epoch AND c.id = NEW."of" AND c.tier = NEW.tier) THEN
        RAISE EXCEPTION 'card % is not of its producer %''s tier', NEW.id, NEW."of"
            USING ERRCODE = 'check_violation', CONSTRAINT = 'cards_consumer_tier_check';
    END IF;
    IF NEW."of" IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sprint.cards c WHERE c.epoch = NEW.epoch AND c.id = NEW."of" AND c.stream = NEW.stream) THEN
        RAISE EXCEPTION 'card % is not in its producer %''s stream', NEW.id, NEW."of"
            USING ERRCODE = 'check_violation', CONSTRAINT = 'cards_consumer_stream_check';
    END IF;
    RETURN NEW;
END
$$;
CREATE OR REPLACE TRIGGER cards_fixed BEFORE INSERT OR UPDATE ON sprint.cards
    FOR EACH ROW EXECUTE FUNCTION sprint.cards_fixed();

-- A stream's, row's or judgement's key (TG_ARGV[0]) and epoch never change:
-- an update that moved one would move its history with it.
CREATE OR REPLACE FUNCTION sprint.key_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF to_jsonb(NEW)->TG_ARGV[0] IS DISTINCT FROM to_jsonb(OLD)->TG_ARGV[0] OR NEW.epoch IS DISTINCT FROM OLD.epoch THEN
        RAISE EXCEPTION '% % changes its % or epoch; they are fixed when it is added', TG_TABLE_NAME, to_jsonb(OLD)->>TG_ARGV[0], TG_ARGV[0]
            USING ERRCODE = 'check_violation', CONSTRAINT = TG_TABLE_NAME || '_key_fixed_check';
    END IF;
    RETURN NEW;
END
$$;
CREATE OR REPLACE TRIGGER streams_key_fixed BEFORE UPDATE ON sprint.streams FOR EACH ROW EXECUTE FUNCTION sprint.key_fixed('name');
CREATE OR REPLACE TRIGGER rows_key_fixed BEFORE UPDATE ON sprint.rows FOR EACH ROW EXECUTE FUNCTION sprint.key_fixed('name');

-- Append-only: one row per operation that entered the store, accepted or
-- refused. Writers run one at a time, so seq is commit order with no gaps.
CREATE TABLE IF NOT EXISTS sprint.events (
    seq      bigint      PRIMARY KEY,
    op       text        NOT NULL UNIQUE,
    actor    text        NOT NULL,
    kind     text        NOT NULL,
    accepted boolean     NOT NULL,
    reason   text        NOT NULL DEFAULT '',
    before   jsonb,
    after    jsonb,
    receipt  jsonb       NOT NULL,
    at       timestamptz NOT NULL,
    epoch    integer     NOT NULL,
    policy   integer     NOT NULL DEFAULT 0 -- the active policy version when the op was checked; 0 when none
);

-- The log is append-only: an event is never changed or removed.
CREATE OR REPLACE FUNCTION sprint.events_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'event % is in the log; the log is append-only', OLD.seq
        USING ERRCODE = 'check_violation', CONSTRAINT = 'events_append_only_check';
END
$$;
CREATE OR REPLACE TRIGGER events_append_only BEFORE UPDATE OR DELETE ON sprint.events
    FOR EACH ROW EXECUTE FUNCTION sprint.events_append_only();

-- Global, not epoch scoped: a clear keeps the policy. At most one version is
-- active. Filled by layer 5.
CREATE TABLE IF NOT EXISTS sprint.policy (
    version serial      PRIMARY KEY,
    kind    text        NOT NULL CHECK (kind IN ('script', 'data')),
    name    text        NOT NULL,
    body    text        NOT NULL,
    active  boolean     NOT NULL DEFAULT false,
    actor   text        NOT NULL,
    at      timestamptz NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS policy_one_active ON sprint.policy (active) WHERE active;

-- A question owed to a person, stored so it outlives any push (promises 1, 5
-- and 6). No gen: JudgementOpen fences it. Epoch scoped: a clear does not
-- carry it. An open judgement is not closed; a closed one has a closed_at.
CREATE TABLE IF NOT EXISTS sprint.judgements (
    id        text        NOT NULL,
    kind      text        NOT NULL,
    about     text        NOT NULL DEFAULT '',
    question  text        NOT NULL DEFAULT '',
    command   text        NOT NULL DEFAULT '',
    group_key text        NOT NULL DEFAULT '',
    state     text        NOT NULL CHECK (state IN ('open', 'answered', 'expired')),
    deadline  timestamptz,
    answer    text        NOT NULL DEFAULT '',
    actor     text        NOT NULL DEFAULT '',
    opened_at timestamptz NOT NULL,
    closed_at timestamptz,
    epoch     integer     NOT NULL,
    PRIMARY KEY (epoch, id),
    CONSTRAINT judgements_closed_check CHECK ((state = 'open') = (closed_at IS NULL)),
    CONSTRAINT judgements_order_check CHECK (closed_at >= opened_at)
);
CREATE OR REPLACE TRIGGER judgements_key_fixed BEFORE UPDATE ON sprint.judgements FOR EACH ROW EXECUTE FUNCTION sprint.key_fixed('id');

-- A subscriber's place in the event log (the last seq it read), so a restart
-- resumes where it stopped (promise 6). Global, as seq is: a clear carries
-- it.
CREATE TABLE IF NOT EXISTS sprint.cursors (
    name text   PRIMARY KEY,
    seq  bigint NOT NULL CHECK (seq >= 0)
);
