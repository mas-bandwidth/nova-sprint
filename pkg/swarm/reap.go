package swarm

import (
	"path/filepath"
)

// SHARED PER-BENCH CACHES (issue #1048). A native job downloads the Go toolchain and every
// module into its own sandboxed data home -- up to 5 GB per slot -- and 120 cards filled
// the benches to 100%. The toolchain and the module cache are the same for every job
// under one swarm root, so they live once under <root>/cache and every job's child is
// pointed at them (GOMODCACHE, GOCACHE, NPM_CONFIG_CACHE). The root is a permitted write
// root beside the job directory (docs/SPEC-SANDBOX.md), never the job's own data home.
const (
	// CacheDirName is the one shared cache directory under a swarm root.
	CacheDirName = "cache"
)

// CacheRoot is the shared cache directory under a swarm root.

// GoModCacheDir is the shared module cache (GOMODCACHE) under a swarm root: the one name
// native hands the child, so the directory made here is the one the child uses.

// GoBuildCacheDir is the shared build cache (GOCACHE) under a swarm root: the one the child
// is handed, JOB.md names and the member's lazy cleaner holds under gocache.Limit.
func GoBuildCacheDir(root string) string { return filepath.Join(root, CacheDirName, "go-build") }

// NPMCacheDir is the shared npm cache (NPM_CONFIG_CACHE) under a swarm root.

// EnsureCacheDirs makes the three shared cache directories, so the child's first cache write
// lands in a directory that exists inside the write set.
