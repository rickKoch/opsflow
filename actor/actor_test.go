package actor

import (
	"context"
	"testing"
	"time"
)

type noOpStorage struct{}

func (noOpStorage) Save(context.Context, string, interface{}) error   { return nil }
func (noOpStorage) Load(context.Context, string) (interface{}, error) { return nil, nil }
func (noOpStorage) Delete(context.Context, string) error              { return nil }

func TestStatelessActor_BasicCall(t *testing.T) {
	mgr := NewVirtualActorManager()
	mgr.RegisterActor("stateless", func(ctx context.Context, input interface{}) (interface{}, error) {
		return "ok", nil
	})
	msgCh := make(chan Message, 1)
	mgr.CallActor(context.Background(), "stateless", nil, msgCh)
	msg := <-msgCh
	result := msg.Payload.(struct {
		Result interface{}
		Error  interface{}
	}).Result.(string)
	if result != "ok" {
		t.Fatalf("unexpected result: got %v", result)
	}
}

func TestActor_Persistence_FileBased(t *testing.T) {
	dir := t.TempDir()
	storage, err := NewFileActorStorage(dir)
	if err != nil {
		t.Fatalf("file storage error: %v", err)
	}
	mgr := NewVirtualActorManager()
	mgr.SetStorage(storage)
	mgr.RegisterActor("stater", func(ctx context.Context, input interface{}) (interface{}, error) {
		var state int
		if input != nil {
			switch n := input.(type) {
			case int:
				state = n
			case float64:
				state = int(n)
			}
		}
		state++
		return StateResult{State: state, Output: state}, nil
	})
	msgCh := make(chan Message, 1)
	mgr.CallActor(context.Background(), "stater", nil, msgCh)
	msg := <-msgCh
	one := msg.Payload.(struct {
		Result interface{}
		Error  interface{}
	}).Result.(int)
	if one != 1 {
		t.Fatalf("expected 1, got %v", one)
	}
	time.Sleep(virtualActorInactivity + 50*time.Millisecond)
	mgr.CallActor(context.Background(), "stater", nil, msgCh)
	msg = <-msgCh
	two := msg.Payload.(struct {
		Result interface{}
		Error  interface{}
	}).Result.(int)
	if two != 2 {
		t.Fatalf("expected restored state, got %v", two)
	}
}

func TestActor_ErrorOnUnknown(t *testing.T) {
	mgr := NewVirtualActorManager()
	msgCh := make(chan Message, 1)
	mgr.CallActor(context.Background(), "missing", "yo", msgCh)
	msg := <-msgCh
	if msg.Payload.(struct {
		Result interface{}
		Error  interface{}
	}).Error != "actor not found" {
		t.Fatalf("expected error for unknown actor")
	}
}
