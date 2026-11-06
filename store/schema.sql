-- The sprint store: layer 1 of nova-sprint v2 (docs/LAYER-1-STORE.md).
-- Idempotent: applying it twice is a no-op. Migrate checks sprint.meta before
-- it runs this, and refuses a store at another schema version. Nothing is
-- ever deleted; a clear increments sprint.epoch and every stream, card and row
-- carries the epoch it was written in. Version 2 adds sprint.cards.author and
-- sprint.cards.base_sha (layer 2); there is no upgrade path from version 1.

CREATE SCHEMA IF NOT EXISTS sprint;

CREATE TABLE IF NOT EXISTS sprint.meta (
    id     integer PRIMARY KEY CHECK (id = 1),
    schema integer NOT NULL
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
    state   text    NOT NULL CHECK (state IN ('up', 'down', 'blocked')),
    session text    NOT NULL DEFAULT '',
    flaps   integer NOT NULL DEFAULT 0,
    epoch   integer NOT NULL,
    PRIMARY KEY (epoch, name)
);

-- One NOT NULL state column with a CHECK and one holder column with a
-- foreign key: a card is in exactly one state and has at most one holder, by
-- the schema.
CREATE TABLE IF NOT EXISTS sprint.cards (
    id      text    NOT NULL,
    stream  text    NOT NULL,
    state   text    NOT NULL CHECK (state IN ('waiting', 'ready', 'working', 'review', 'merging', 'landed', 'left')),
    gen     integer NOT NULL DEFAULT 0,
    attempt integer NOT NULL DEFAULT 0,
    score   integer NOT NULL DEFAULT 0,
    needs   text[]  NOT NULL DEFAULT '{}',
    holder  text,
    branch  text    NOT NULL DEFAULT '',
    head    text    NOT NULL DEFAULT '',
    pr      integer NOT NULL DEFAULT 0,
    brief   text    NOT NULL DEFAULT '',
    reason  text    NOT NULL DEFAULT '',
    author   text   NOT NULL DEFAULT '',
    base_sha text   NOT NULL DEFAULT '',
    epoch   integer NOT NULL,
    PRIMARY KEY (epoch, id),
    FOREIGN KEY (epoch, stream) REFERENCES sprint.streams (epoch, name),
    FOREIGN KEY (epoch, holder) REFERENCES sprint.rows (epoch, name)
);

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
