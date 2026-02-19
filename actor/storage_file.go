package actor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileActorStorage persists actor state in per-actor files in a chosen directory.
type FileActorStorage struct {
	dir string // Directory for storing per-actor json state
}

// NewFileActorStorage creates a new file-based storage in dir, creating dir if needed.
func NewFileActorStorage(dir string) (*FileActorStorage, error) {
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, err
	}
	return &FileActorStorage{dir: dir}, nil
}

func (fs *FileActorStorage) fname(name string) string {
	return filepath.Join(fs.dir, fmt.Sprintf("%s.json", name))
}

func (fs *FileActorStorage) Save(ctx context.Context, actorName string, state interface{}) error {
	if state == nil {
		return nil
	}
	f, err := os.Create(fs.fname(actorName))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(state)
}

func (fs *FileActorStorage) Load(ctx context.Context, actorName string) (interface{}, error) {
	fn := fs.fname(actorName)
	f, err := os.Open(fn)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var state interface{}
	dec := json.NewDecoder(f)
	err = dec.Decode(&state)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (fs *FileActorStorage) Delete(ctx context.Context, actorName string) error {
	fn := fs.fname(actorName)
	return os.Remove(fn)
}
