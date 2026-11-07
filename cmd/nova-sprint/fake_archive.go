package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/mas-bandwidth/nova-sprint/run/filestore"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

type fakeArchive struct {
	source    store.Store
	directory string
}
type fakeImage struct {
	Version  int            `json:"version"`
	Snapshot store.Snapshot `json:"snapshot"`
	Events   []store.Event  `json:"events"`
}

func (a fakeArchive) Backup(ctx context.Context, dir string) (verbs.BackupInfo, error) {
	snap, err := a.source.Snapshot(ctx)
	if err != nil {
		return verbs.BackupInfo{}, err
	}
	events, err := fakeHistory(ctx, a.source, snap.Seq)
	if err != nil {
		return verbs.BackupInfo{}, err
	}
	if !reflect.DeepEqual(store.Rebuild(events), snap) {
		return verbs.BackupInfo{}, fmt.Errorf("fake backup: history does not match snapshot")
	}
	body, err := json.Marshal(fakeImage{Version: filestore.FormatVersion, Snapshot: snap, Events: events})
	if err != nil {
		return verbs.BackupInfo{}, err
	}
	if dir == "" {
		dir = a.directory
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return verbs.BackupInfo{}, err
	}
	path, err := writeFakeBackup(dir, body)
	if err != nil {
		return verbs.BackupInfo{}, err
	}
	verified, err := a.Verify(ctx, path)
	if err != nil {
		return verbs.BackupInfo{}, err
	}
	if verified.Seq != snap.Seq || verified.Epoch != snap.Epoch {
		return verbs.BackupInfo{}, fmt.Errorf("fake backup: verification mismatch")
	}
	return verified, nil
}
func fakeHistory(ctx context.Context, s store.Store, target int64) ([]store.Event, error) {
	var result []store.Event
	for seq := int64(0); seq < target; {
		page, err := s.Events(ctx, seq, store.MaxEvents)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return nil, fmt.Errorf("fake backup: history ended before snapshot")
		}
		for _, event := range page {
			if event.Seq != seq+1 {
				return nil, fmt.Errorf("fake backup: sequence gap")
			}
			if event.Seq > target {
				break
			}
			result = append(result, event)
			seq = event.Seq
		}
	}
	return result, nil
}
func writeFakeBackup(dir string, body []byte) (string, error) {
	file, err := os.CreateTemp(dir, "fake-backup-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	okay := false
	defer func() {
		_ = file.Close()
		if !okay {
			_ = os.Remove(path)
		}
	}()
	if _, err = file.Write(body); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	okay = true
	return path, nil
}
func (a fakeArchive) Verify(ctx context.Context, path string) (verbs.BackupInfo, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return verbs.BackupInfo{}, fmt.Errorf("fake backup: file unavailable")
	}
	f, err := filestore.Open(path)
	if err != nil {
		return verbs.BackupInfo{}, fmt.Errorf("fake backup: verify: %w", err)
	}
	snap, err := f.Snapshot(ctx)
	if err != nil {
		return verbs.BackupInfo{}, err
	}
	return verbs.BackupInfo{Path: path, Epoch: snap.Epoch, Seq: snap.Seq}, nil
}
func (fakeArchive) AcquireRestore(context.Context) (verbs.RestoreLease, error) {
	return nil, fmt.Errorf("fake restore is not available; retain the backup file")
}
func fakeBackupDir(path string) string { return filepath.Clean(path) + ".backups" }
