package roster

// The real Source reads nova-config's PostgreSQL through nova-tools'
// config package (github.com/mas-bandwidth/nova-tools/v2/config/lib,
// v2.0.3). Layer 6 owns it (modules/run/config_source.go): run opens one
// config.Store, takes one config Snapshot per tick (one REPEATABLE READ
// transaction for machines and friends together) and hands it to Load as a
// Source. roster imports no database driver; Fake (fake.go) is the tests'.
