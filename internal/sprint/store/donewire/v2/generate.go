// Package donewire is a newer-layout test fixture of schemas/store/store.schema:
// DoneRecord with wrote int64 appended, locked over schemas/store/schema.lock.
package donewire

//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.1-0.20261017153557-54e0eda7d20b generate --lang go --out . .
