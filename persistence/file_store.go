package persistence

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// FileStore is a simple file-based persistence implementation.
type FileStore struct {
	Root string
}

func NewFileStore(root string) *FileStore {
	_ = os.MkdirAll(root, 0o755)
	return &FileStore{Root: root}
}

func (f *FileStore) snapshotPath(id PID) string {
	return filepath.Join(f.Root, string(id)+".snapshot.json")
}

func (f *FileStore) mailboxPath(id PID) string {
	return filepath.Join(f.Root, string(id)+".mailbox.json")
}

func (f *FileStore) SaveSnapshot(ctx context.Context, id PID, data []byte) error {
	// if data is nil, remove file
	p := f.snapshotPath(id)
	if data == nil {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return nil
		}
		return os.Remove(p)
	}
	return ioutil.WriteFile(p, data, 0o644)
}

func (f *FileStore) LoadSnapshot(ctx context.Context, id PID) ([]byte, error) {
	p := f.snapshotPath(id)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, nil
	}
	return ioutil.ReadFile(p)
}

// ListKeys lists snapshot files with the given prefix. It returns PIDs.
func (f *FileStore) ListKeys(ctx context.Context, prefix PID) ([]PID, error) {
	files, err := ioutil.ReadDir(f.Root)
	if err != nil {
		return nil, err
	}
	var out []PID
	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		name := fi.Name()
		if strings.HasPrefix(name, string(prefix)) && strings.HasSuffix(name, ".snapshot.json") {
			base := strings.TrimSuffix(name, ".snapshot.json")
			out = append(out, PID(base))
		}
	}
	return out, nil
}

func (f *FileStore) SaveMailbox(ctx context.Context, id PID, messages [][]byte) error {
	b, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(f.mailboxPath(id), b, 0o644)
}

func (f *FileStore) LoadMailbox(ctx context.Context, id PID) ([][]byte, error) {
	p := f.mailboxPath(id)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, nil
	}
	b, err := ioutil.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var msgs [][]byte
	if err := json.Unmarshal(b, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}
