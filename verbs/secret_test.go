package verbs

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

type terminalFake struct {
	asks int
	data []byte
}

func (t *terminalFake) ReadHidden(context.Context, string) ([]byte, error) {
	t.asks++
	return append([]byte{}, t.data...), nil
}

type sinkFake struct {
	saved bool
	fail  bool
	value string
	name  string // the name the value was saved under
}

func (s *sinkFake) Has(context.Context, string) (bool, error) { return s.saved, nil }
func (s *sinkFake) Set(_ context.Context, name string, r io.Reader) error {
	if s.fail {
		return errors.New("failed")
	}
	b, _ := io.ReadAll(r)
	s.value = string(b)
	s.saved = true
	s.name = name
	return nil
}
func TestKeyAskedAgainOnlyBeforeDurableSave(t *testing.T) {
	t.Parallel()
	terminal := &terminalFake{data: []byte("fake-key")}
	sink := &sinkFake{fail: true}
	ctx := context.Background()
	if EnsureKey(ctx, "acme", "ACME_API_KEY", terminal, sink) == nil {
		t.Fatal("failed save accepted")
	}
	sink.fail = false
	if err := EnsureKey(ctx, "acme", "ACME_API_KEY", terminal, sink); err != nil {
		t.Fatal(err)
	}
	if err := EnsureKey(ctx, "acme", "ACME_API_KEY", terminal, sink); err != nil {
		t.Fatal(err)
	}
	if terminal.asks != 2 || sink.value != "fake-key" || sink.name != "ACME_API_KEY" {
		t.Fatal(terminal.asks, sink.value, sink.name)
	}
}

type terminalRace struct{ sink *sinkFake }

func (t terminalRace) ReadHidden(context.Context, string) ([]byte, error) {
	t.sink.saved = true
	t.sink.value = "already-saved"
	return []byte("new-key"), nil
}
func TestKeySavedDuringPromptIsNotOverwritten(t *testing.T) {
	t.Parallel()
	sink := &sinkFake{}
	if err := EnsureKey(context.Background(), "acme", "ACME_API_KEY", terminalRace{sink}, sink); err != nil {
		t.Fatal(err)
	}
	if sink.value != "already-saved" {
		t.Fatal(sink.value)
	}
}

func TestTTYSignalHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_TTY_HELPER") != "1" {
		t.Skip("PTY subprocess only")
	}
	_, err := (TTY{}).ReadHidden(context.Background(), "hidden: ")
	if err == nil {
		t.Fatal("expected interrupted prompt")
	}
}
