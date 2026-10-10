module github.com/mas-bandwidth/nova-sprint

go 1.26.6

tool (
	github.com/mas-bandwidth/nova-tools/tools/ci
	github.com/mas-bandwidth/nova-tools/tools/tlacheck
	golang.org/x/tools/cmd/deadcode
)

require (
	github.com/redis/go-redis/v9 v9.22.0
	github.com/stretchr/testify v1.12.1
	go.uber.org/goleak v1.3.0
	golang.org/x/mod v0.41.0
)

require (
	github.com/jackc/pgx/v5 v5.11.0 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mas-bandwidth/nova-tools v1.2.3-0.20261010170435-a46dbf216ee7
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/telemetry v0.0.0-20260908163034-4bcc4b2ee518 // indirect
	golang.org/x/tools v0.50.0 // indirect
)
