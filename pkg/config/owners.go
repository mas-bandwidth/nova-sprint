package config

// Ownership is what the catalog says about schema config for the role a
// store is connected as (Store.Ownership reads it in one query). migrate's
// preflight decides from it, before anything is applied, whether that role
// can apply the pending migrations (MigrateGaps).
type Ownership struct {
	// Role is the connected role (current_user).
	Role string
	// SchemaOwner owns schema config; "" when the schema does not exist yet.
	SchemaOwner string
	// Create is whether Role may create tables in schema config.
	Create bool
	// Tables is each table of schema config and the role that owns it.
	Tables map[string]string
}

// Pending is the migrations of all (in version order, as Migrations lists
// them) after version from.
func Pending(all []Migration, from int) []Migration {
	var out []Migration
	for _, m := range all {
		if m.Version > from {
			out = append(out, m)
		}
	}
	return out
}

// catalogTables is every table Mem stands in for: each kind's, the ledger and
// the history, as a migrated schema config has.
func catalogTables(role string) map[string]string {
	out := map[string]string{"schema_migrations": role, "history": role}
	for _, k := range Kinds {
		out[k.Table] = role
	}
	return out
}

// Session is another backend holding the store's database, as the catalog
// (pg_stat_activity) shows it to the connected role: the role, the client's
// application name when it set one, and the backend's pid. migrate --window
// refuses while any nova role but its own session holds the database: the
// window of the seat play (fleet/tools.yml) has the old server and member
// stopped, and a migration that is not an addition the old build tolerates
// must never run under one.
type Session struct {
	Role        string
	Application string
	PID         int
}
