# Connect the coordinator's local commands

Most coordinator commands go through the sprint server and need only its
address and the coordinator actor. A few commands, including `fleet sync`,
`friend sync`, and `nova-config`, execute locally and need access to the
configuration store. This guide builds the credential wrapper for those
commands without putting secret values into the command line.

Start with [the coordinator's guide](SPRINT-COORDINATOR.md#1-the-seat).
The examples here use a Nova Tools deployment with a supervised sprint server,
`jq`, `sops`, and the Nova Tools commands on `PATH`. They are deployment
recipes, not prerequisites for the [local simulation](GETTING-STARTED.md).

## Read the deployment values

Find the server loop with `nova-config loop list` and inspect its unit on
the coordinator machine. On macOS the unit is under
`~/Library/LaunchAgents/com.nova.loop.sprint-server-<m>.plist`; on Linux use
`systemctl --user cat nova-loop-sprint-server-<m>.service`.
The [Nova Tools fleet guide](https://github.com/mas-bandwidth/nova-tools/blob/dev/docs/FLEET.md)
explains those layouts.

For a macOS coordinator, select the unit for this sprint, then extract the
argument values. `RPW` is the name of a password environment variable, not
the password itself:

```sh
U=~/Library/LaunchAgents/com.nova.loop.sprint-server-<m>.plist
A=$(plutil -extract ProgramArguments json -o - "$U")
word() { echo "$A" | jq -r --arg k "$1" '.[index($k)+1]'; }
envw() { echo "$A" | jq -r --arg k "$1=" '.[]|select(startswith($k))|sub($k;"")'; }
STORE=$(word --store); SEAT=$(word --as); KEY=$(word --key); PORT=$(word --listen | sed 's/.*://')
REDIS=$(envw NOVA_SPRINT_REDIS); RUSER=$(envw NOVA_SPRINT_REDIS_USER); RPW=$(envw NOVA_SPRINT_REDIS_PASSWORD_ENV)
export NOVA_SPRINT_SERVER=127.0.0.1:$PORT
export NOVA_SPRINT_ACTOR=$(nova-sprint where --json | jq -r .coordinator)
```

Read these values from the current deployment each time you take over. If a
wrapper refuses access, investigate that refusal rather than substituting
credentials from memory. The output shapes below let you recognise a result
without printing secret values.

## Find the configuration connection

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

## Run a command through the wrapper

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

## Deployment gaps to be aware of

- The directory of the secrets store, when no unit names it. `find ~ -maxdepth 3 -name .sops.yaml` finds
  candidates, one per store of every account on the machine, and the store is the one whose `nova-secrets names`
  lists the seat's names (nova-tools#5152).
- The map from a config role to its secret name (above, nova-tools#5152).
- The unit's linux form (`systemctl --user cat nova-loop-sprint-server-<m>.service`) was not run for this file;
  everything above ran on a darwin coordinator machine.
