package pgbin

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Options are the ways the functional tests' servers differ. The zero value
// is the common server: superuser nova, trust auth, fsync off, stopped with
// pg_ctl -m fast.
type Options struct {
	Bin      string   // the directory holding initdb and pg_ctl; Dir(t) when empty
	User     string   // the superuser initdb makes; "nova" when empty
	Settings []string // more server settings, "name=value", after the defaults (a later one wins)
	Stop     string   // pg_ctl's stop mode; "fast" when empty
	Socket   bool     // listen on a Unix socket in Server.Dir too (only TCP when false)
	HBA      string   // pg_hba.conf's contents, when not initdb's
}

// Server is a throwaway PostgreSQL of one test's own.
type Server struct {
	Dir  string // the test's directory for it: the data directory and the logs
	Data string // the data directory
	Port int
	DSN  string // postgres://USER@127.0.0.1:PORT/postgres?sslmode=disable
	// Socket is the keyword/value connection string over the Unix socket,
	// when Options.Socket: "host=DIR port=PORT user=USER dbname=postgres sslmode=disable".
	Socket string
}

// Start initdbs a server into a t.TempDir(), starts it with pg_ctl on a free
// port of 127.0.0.1, waits until it takes a connection, and stops it when t
// ends. Its data directory and its port are its own, so tests can run many
// at once on one host. A port free when looked up can be taken before the
// postmaster binds it (by another test's server, or a client connection's
// local port); the postmaster then exits, and a start whose log says so is
// tried again on a new port. Any other failure fails t, with the server's log.
func Start(t testing.TB, o Options) Server {
	t.Helper()
	if o.Bin == "" {
		o.Bin = Dir(t)
	}
	if o.User == "" {
		o.User = "nova"
	}
	if o.Stop == "" {
		o.Stop = "fast"
	}
	s := Server{Dir: t.TempDir()}
	s.Data = filepath.Join(s.Dir, "data")
	// LC_ALL=C: PostgreSQL 18 on macOS refuses to start under a shell with no
	// valid locale ("postmaster became multithreaded during startup").
	env := append(os.Environ(), "LC_ALL=C")
	run := func(name string, args ...string) ([]byte, error) {
		cmd := exec.Command(filepath.Join(o.Bin, name), args...)
		cmd.Env = env
		return cmd.CombinedOutput()
	}
	if out, err := run("initdb", "-D", s.Data, "-U", o.User, "--auth=trust", "-E", "UTF8", "--no-locale"); err != nil {
		t.Fatalf("initdb: %v\n%s", err, out)
	}
	if o.HBA != "" {
		if err := os.WriteFile(filepath.Join(s.Data, "pg_hba.conf"), []byte(o.HBA), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Stop is registered before start: a start that fails or times out can
	// leave a postmaster running, and t.Fatalf must not orphan it.
	t.Cleanup(func() {
		if out, err := run("pg_ctl", "-D", s.Data, "-m", o.Stop, "-w", "stop"); err != nil {
			t.Logf("pg_ctl stop: %v\n%s", err, out)
		}
	})
	socket := "-c unix_socket_directories="
	if o.Socket {
		socket = "-k " + s.Dir
	}
	settings := "-c listen_addresses=127.0.0.1 " + socket + " -c fsync=off"
	for _, kv := range o.Settings {
		settings += " -c " + kv
	}
	for try := 1; ; try++ {
		s.Port = freePort(t)
		logf := filepath.Join(s.Dir, "postgres-"+strconv.Itoa(try)+".log")
		out, err := run("pg_ctl", "-D", s.Data, "-l", logf, "-o", "-p "+strconv.Itoa(s.Port)+" "+settings, "-w", "start")
		if err == nil {
			break
		}
		log, _ := os.ReadFile(logf) // ignored: an unreadable log is reported empty
		if try < 5 && strings.Contains(string(log), "could not bind") {
			t.Logf("pg_ctl start: port %d was taken; trying another\n%s", s.Port, log)
			continue
		}
		t.Fatalf("pg_ctl start: %v\n%s\npostgres log:\n%s", err, out, log)
	}
	s.DSN = fmt.Sprintf("postgres://%s@127.0.0.1:%d/postgres?sslmode=disable", o.User, s.Port)
	probe := s.DSN
	if o.Socket {
		s.Socket = fmt.Sprintf("host=%s port=%d user=%s dbname=postgres sslmode=disable", s.Dir, s.Port, o.User)
		probe = s.Socket // a pg_hba.conf of the test's may ask a password over TCP
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		c, err := pgx.Connect(context.Background(), probe)
		if err == nil {
			_ = c.Close(context.Background()) // ignored: only probing
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("postgres did not take a connection in 30s: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// freePort is a port free on 127.0.0.1 when looked up.
func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
