package persistence

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
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
	return ioutil.WriteFile(f.snapshotPath(id), data, 0o644)
}

func (f *FileStore) LoadSnapshot(ctx context.Context, id PID) ([]byte, error) {
	return ioutil.ReadFile(f.snapshotPath(id))
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
