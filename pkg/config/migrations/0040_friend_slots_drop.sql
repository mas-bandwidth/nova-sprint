-- 0040: a friend row is width only (pkg/config/kind.go: Kinds, "friend";
-- docs/SPEC-CONFIG.md, friend). The owner, 2026-10-13: "there is no ceiling
-- for friend slots per-machine. i never asked for one!" The slots column is
-- dropped: the friend's own width is her row, and no machine ceiling bounds
-- her. The file run again skips the clause.
ALTER TABLE config.friends DROP COLUMN IF EXISTS slots;
