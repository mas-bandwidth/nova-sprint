# The coordinator seat: the wrapper for the verbs the server does not serve

[SPRINT-COORDINATOR.md](SPRINT-COORDINATOR.md) section 1 reads the seat's values from the sprint server's loop
unit: the shell variables `STORE`, `SEAT`, `KEY`, `REDIS`, `RUSER`, `RPW` and `PORT` below come from there, and
`NOVA_SPRINT_SERVER` and `NOVA_SPRINT_ACTOR` are exported. A served verb needs nothing more. `fleet sync`,
`friend sync` and `nova-config` read the config store, which needs two more values and a wrapper. No value is
typed from memory and none is printed: each command below prints a shape, shown after it.

## The two values

```
PGPW=$(nova-secrets names --store "$STORE" --as "$SEAT" | sed -n 's/^SECRETS NAME key=\([A-Z_]*PG_CONFIG[A-Z_]*\) .*/\1/p')
REDISENV="NOVA_SPRINT_REDIS=$REDIS NOVA_SPRINT_REDIS_USER=$RUSER NOVA_SPRINT_REDIS_PASSWORD_ENV=$RPW"
DSN=$(nova-secrets exec --store "$STORE" --as "$SEAT" --key "$KEY" --sops "$(command -v sops)" \
  --only "$RPW" --require="$RPW" -- env $REDISENV nova-config inventory --host "$(nova-config machine self)" \
  2>/dev/null | jq -r .nova_pg_dsn)
```

- `PGPW` is the name of the secret that holds the config role's password. `nova-secrets names` lists the
  seat's names, one per line, never a value: `SECRETS NAME key=<NAME> clear=false`, ending in
  `SECRETS NAMES OK as=<seat> keys=<n> ...`. The role's is the name that says so; no command maps a role to a
  name (nova-tools#5152).
- `nova-config machine self` prints one word, this machine's name in the inventory. `nova-config inventory
  --host <m>` prints one JSON object (`ansible_*`, `kind`, `nova_loops`, `nova_pg_dsn`, `nova_redis_addr`,
  `nova_redis_port`, `nova_seat`, `runners`, `slots`), and `nova_pg_dsn` is `postgres://<role>@<host:port>/<db>`
  with no password. It is read from Redis with the coordinator's Redis variables, the only secret opened.

## The wrapper

```
nova-secrets exec --store "$STORE" --as "$SEAT" --key "$KEY" --sops "$(command -v sops)" \
  --only "$PGPW,$RPW" --require="$PGPW" --require="$RPW" -- \
  env NOVA_PG_DSN="$DSN" NOVA_PG_PASSWORD_ENV="$PGPW" $REDISENV NOVA_SPRINT_ACTOR="$NOVA_SPRINT_ACTOR" \
  nova-sprint fleet sync --check
```

- The first line printed is `SECRETS EXEC OK as=<seat> keys=2 only=2 required=2 file=<seat file> head=<commit>
  cmd=<program>`, then the verb's own lines: `FLEET-SYNC CHECK OK drift=<n> members=<n> ...` (exit 0, none; 2
  drift; 3 the config cannot be read). Any other `nova-sprint` verb that is not served, or `friend sync`, takes
  the place of `fleet sync --check`; unset the three config words and `--only "$PGPW"` for a verb that needs only
  the sprint's store.
- `nova-config` takes the same wrapper with `--only "$PGPW" --require="$PGPW"` and
  `env NOVA_PG_DSN="$DSN" NOVA_PG_PASSWORD_ENV="$PGPW" nova-config <verb>`; `nova-config apply` also takes
  `$REDISENV` and `--only "$PGPW,$RPW"`. Reads print one line per row: `MACHINE name=<m> user=<u> seat=<seat>
  slots=<n> runners=<n> width=<n>`, `FLEET name=fleet store=<m> coordinator=<m> redis_port=<port> pg_dsn=<dsn>
  ...`, `LOOP name=<loop> machine=<m> argv=<json>`, each ending in a `CONFIG LIST kind=<kind> rows=<n>` line for a
  list.

## What has no command

- The directory of the secrets store, when no unit names it. `find ~ -maxdepth 3 -name .sops.yaml` finds
  candidates, one per store of every account on the machine, and the store is the one whose `nova-secrets names`
  lists the seat's names (nova-tools#5152).
- The map from a config role to its secret name (above, nova-tools#5152).
- The unit's linux form (`systemctl --user cat nova-loop-sprint-server-<m>.service`) was not run for this file;
  everything above ran on a darwin coordinator machine.

## The hook under a Monitor

The seat's session runs `nova-hook hook --as "$SEAT" --role coordinator` under a Monitor, with `NOVA_BUS_REDIS`
(and its login's variables) exported: nova-hook holds the seat's bus push and prints one event a line, each
message to the seat on the bus (the sprint's server sends every judgment and note for the coordinator there,
`nova-sprint: <n> new for the coordinator`, with the commands that answer each) and each `HOOK CHALLENGE`, which
the seat answers from inside its turn with `nova-hook prove --as "$SEAT" --nonce <nonce>`. `nova-hook unhook --as
"$SEAT"` ends it. The coordinator's sprint push is held by the sprint's server (`nova-sprint run --listen`) while it
runs and reaches the bus; the sprint's own hook, its compact form and the seat's push loop are retired (v1.3.6).


### Seat environment defaults

`nova-up --seat` writes `$XDG_CONFIG_HOME/nova-sprint/seat-environment.json`, or
`$HOME/.config/nova-sprint/seat-environment.json` when `XDG_CONFIG_HOME` is unset.
`nova-sprint` reads this private, owned regular file on startup. No wrapper or
shell sourcing is required. An absent file preserves the existing setup; an
invalid or unsafe file refuses the command without printing its contents.

Version 1 is JSON data with `version: 1`, an optional `environment` object
containing only `NOVA_SPRINT_REDIS`, `NOVA_SPRINT_SERVER`, and `NOVA_BUS_REDIS`,
and an optional complete `login` object with `redis`, `user`, `store`, `as`,
`key`, `sops`, and `secret`. The login names where a password is sealed; it
contains no password. It applies only to its exact Redis address, for either
sprint or bus. An existing `login.json` takes precedence over this login.
Explicit command flags take precedence over process environment variables,
which take precedence over file endpoint defaults. Explicit process login
variables take precedence over recorded credentials. A password reference
without an explicit ACL user uses the default user and never inherits the
recorded user. The directory holding this profile must also be owned by the
current user and must not be writable by other users.

The optional `forge_identity` is an absolute path to an existing GitHub CLI
configuration directory. It is a reference, not a token or an actor assertion.
`GH_CONFIG_DIR` in the process environment takes precedence. Reading this file
never changes the process environment and never establishes a sprint actor;
actor selection still requires the command's usual identity proof.

Unknown or duplicate fields, unsupported versions, credential-bearing endpoint
URLs, symbolic links, and files accessible to other users are refused. Endpoint
values and secret values are never evaluated as shell code.
