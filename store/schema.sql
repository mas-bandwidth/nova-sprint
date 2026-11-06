-- The sprint store: layer 1 of nova-sprint v2 (docs/LAYER-1-STORE.md).
-- Idempotent: applying it twice is a no-op. Nothing is ever deleted; a clear
-- increments sprint.epoch and every row carries the epoch it was written in.

CREATE SCHEMA IF NOT EXISTS sprint;

-- One row: the current epoch. The unique index on a constant keeps it one row.
CREATE TABLE IF NOT EXISTS sprint.epoch (
    n integer NOT NULL CHECK (n >= 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS epoch_one_row ON sprint.epoch ((true));
INSERT INTO sprint.epoch (n) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM sprint.epoch);

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
    name  text    NOT NULL,
    kind  text    NOT NULL CHECK (kind IN ('worker', 'kin')),
    width integer NOT NULL DEFAULT 1,
    state text    NOT NULL CHECK (state IN ('up', 'down', 'blocked')),
    seen  timestamptz,
    flaps integer NOT NULL DEFAULT 0,
    epoch integer NOT NULL,
    PRIMARY KEY (epoch, name)
);

-- One state column with a CHECK and one holder column with a foreign key: a
-- card is in exactly one state and has at most one holder, by the schema.
CREATE TABLE IF NOT EXISTS sprint.cards (
    id     text    NOT NULL,
    stream text    NOT NULL,
    state  text    NOT NULL CHECK (state IN ('waiting', 'ready', 'working', 'review', 'merging', 'landed', 'left')),
    gen    integer NOT NULL DEFAULT 0,
    score  integer NOT NULL DEFAULT 0,
    needs  text[]  NOT NULL DEFAULT '{}',
    holder text,
    head   text    NOT NULL DEFAULT '',
    pr     integer NOT NULL DEFAULT 0,
    brief  text    NOT NULL DEFAULT '',
    reason text    NOT NULL DEFAULT '',
    epoch  integer NOT NULL,
    PRIMARY KEY (epoch, id),
    FOREIGN KEY (epoch, stream) REFERENCES sprint.streams (epoch, name),
    FOREIGN KEY (epoch, holder) REFERENCES sprint.rows (epoch, name)
);

-- Append-only: one row per operation asked for, accepted or refused.
CREATE TABLE IF NOT EXISTS sprint.events (
    seq      bigserial   PRIMARY KEY,
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

-- Filled by layer 5; at most one version is active.
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
