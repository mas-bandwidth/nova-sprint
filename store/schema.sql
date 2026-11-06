-- The sprint store: layer 1 of nova-sprint v2 (docs/LAYER-1-STORE.md).
-- Idempotent: applying it twice is a no-op. Migrate checks sprint.meta before
-- it runs this, and refuses a store at another schema version. Nothing is
-- ever deleted; a clear increments sprint.epoch and every stream, card and row
-- carries the epoch it was written in. Version 2 is the version with the two
-- card kinds (kind with sentinel, of, after, author, base_sha, result, the
-- states blocked and done; prior_head, bounces, cost, priced, priority and the
-- result fault) and row gen and held;
-- there is no upgrade path from version 1.

CREATE SCHEMA IF NOT EXISTS sprint;

-- hash is the sha256 of this file as the binary that made the store carries
-- it: Migrate sets it on a new store and refuses a store whose hash differs
-- at the same version (a different build of the schema).
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
    pos   integer NOT NULL DEFAULT 0,
    epoch integer NOT NULL,
    PRIMARY KEY (epoch, name)
);

CREATE TABLE IF NOT EXISTS sprint.rows (
    name    text    NOT NULL,
    kind    text    NOT NULL CHECK (kind IN ('worker', 'kin')),
    width   integer NOT NULL DEFAULT 1,
    state   text    NOT NULL CHECK (state IN ('up', 'down')),
    session text    NOT NULL DEFAULT '',
    flaps   integer NOT NULL DEFAULT 0,
    held    boolean NOT NULL DEFAULT false,
    gen     integer NOT NULL DEFAULT 0,
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
    gen      integer NOT NULL DEFAULT 0,
    attempt  integer NOT NULL DEFAULT 0,
    score    integer NOT NULL DEFAULT 0,
    needs    text[]  NOT NULL DEFAULT '{}',
    after    text[]  NOT NULL DEFAULT '{}',
    holder   text,
    author   text    NOT NULL DEFAULT '',
    branch   text    NOT NULL DEFAULT '',
    base_sha text    NOT NULL DEFAULT '',
    prior_head text  NOT NULL DEFAULT '',
    head     text    NOT NULL DEFAULT '',
    pr       integer NOT NULL DEFAULT 0,
    result   text    NOT NULL DEFAULT '' CHECK (result IN ('', 'ok', 'fix', 'fail', 'fault')),
    bounces  integer NOT NULL DEFAULT 0,
    cost     bigint  NOT NULL DEFAULT 0,
    priced   boolean NOT NULL DEFAULT false,
    priority text    NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'critical', 'blocker')),
    brief    text    NOT NULL DEFAULT '',
    reason   text    NOT NULL DEFAULT '',
    epoch    integer NOT NULL,
    PRIMARY KEY (epoch, id),
    CONSTRAINT cards_of_check CHECK ((kind IN ('producer', 'sentinel')) = ("of" IS NULL)),
    CONSTRAINT cards_needs_check CHECK (kind IN ('producer', 'sentinel') OR needs = '{}'),
    CONSTRAINT cards_after_check CHECK (kind IN ('producer', 'sentinel') OR after = '{}'),
    CONSTRAINT cards_brief_check CHECK (kind <> 'sentinel' OR brief = ''),
    CONSTRAINT cards_consumer_priority_check CHECK (kind IN ('producer', 'sentinel') OR priority = 'normal'),
    CONSTRAINT cards_holder_check CHECK (kind IN ('work', 'read') OR holder IS NULL),
    CONSTRAINT cards_prior_head_check CHECK (kind = 'work' OR prior_head = ''),
    CONSTRAINT cards_bounces_check CHECK (kind = 'producer' OR bounces = 0),
    CONSTRAINT cards_producer_cost_check CHECK (kind IN ('work', 'read') OR (cost = 0 AND NOT priced)),
    CONSTRAINT cards_producer_result_check CHECK (kind IN ('work', 'read') OR result = ''),
    FOREIGN KEY (epoch, stream) REFERENCES sprint.streams (epoch, name),
    FOREIGN KEY (epoch, holder) REFERENCES sprint.rows (epoch, name),
    FOREIGN KEY (epoch, "of") REFERENCES sprint.cards (epoch, id)
);

-- What a CHECK cannot say: a card's kind, of and stream never change once it
-- is written, and a consumer's of names a producer (the foreign key says only
-- that it names a card). A consumer's attempt is fixed at add too, but a
-- trigger cannot tell the store's own writes from a caller's, so that rule is
-- the store's alone.
CREATE OR REPLACE FUNCTION sprint.cards_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.kind IS DISTINCT FROM OLD.kind OR NEW."of" IS DISTINCT FROM OLD."of" OR NEW.stream IS DISTINCT FROM OLD.stream) THEN
        RAISE EXCEPTION 'card % changes its kind, of or stream; they are fixed when the card is added', OLD.id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'cards_fixed_check';
    END IF;
    IF NEW."of" IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sprint.cards c WHERE c.epoch = NEW.epoch AND c.id = NEW."of" AND c.kind = 'producer') THEN
        RAISE EXCEPTION 'card % is a consumer of %, which is not a producer', NEW.id, NEW."of"
            USING ERRCODE = 'check_violation', CONSTRAINT = 'cards_of_producer_check';
    END IF;
    RETURN NEW;
END
$$;
CREATE OR REPLACE TRIGGER cards_fixed BEFORE INSERT OR UPDATE ON sprint.cards
    FOR EACH ROW EXECUTE FUNCTION sprint.cards_fixed();

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
    epoch    integer     NOT NULL
);

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
