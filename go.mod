module github.com/mas-bandwidth/nova-sprint

go 1.26.6

require (
	github.com/jackc/pgx/v5 v5.11.0
	github.com/mas-bandwidth/nova-tools/v2 v2.0.3
	github.com/stretchr/testify v1.12.1
	go.starlark.net v0.0.0-20261021141710-4f4267fb140d
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/telemetry v0.0.0-20260908163034-4bcc4b2ee518 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	honnef.co/go/tools v0.8.1 // indirect
)

tool (
	golang.org/x/tools/cmd/deadcode
	honnef.co/go/tools/cmd/staticcheck
)
