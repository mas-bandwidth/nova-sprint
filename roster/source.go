package roster

// The real Source reads nova-config's PostgreSQL through nova-tools'
// config package. It is not wired yet:
//
// TODO(round 22): when branch rowan/config-friends-library of nova-tools is
// on main, add the Source here: open it with config.Open(ctx, dsn, password)
// and map (*config.Store).Machines(ctx) and (*config.Store).Friends(ctx)
// onto Machine and Friend. Until then the only Source is Fake (fake.go).
