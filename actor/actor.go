package actor

import (
	"context"
	"sync"
	"time"
)

// ActorStorage defines per-actor state persistence hooks (optional).
type ActorStorage interface {
	Save(ctx context.Context, actorName string, state interface{}) error
	Load(ctx context.Context, actorName string) (interface{}, error) // nil,nil if not found
	Delete(ctx context.Context, actorName string) error
}

// StateResult can be used by an actor to distinguish output from persisted state.
type StateResult struct {
	State  interface{}
	Output interface{}
}

// ActorFunc is the user-defined actor handler
type ActorFunc func(ctx context.Context, input interface{}) (output interface{}, err error)

// Message is sent to or from an actor
type Message struct {
	ActorName string
	Payload   interface{}
}

// -- virtual actor system --
type VirtualActorManager struct {
	mu       sync.Mutex
	Actors   map[string]*virtualActorEntry
	storage  ActorStorage // Optional, for actor state persistence
	registry *registry    // Where actor handlers live (private)
}

type virtualActorEntry struct {
	name     string
	msgCh    chan actorMsg
	quitCh   chan struct{}
	lastUsed time.Time
	state    interface{} // For optional persistence
	registry *registry   // Reference for handler lookup
}
type actorMsg struct {
	ctx      context.Context
	input    interface{}
	outputCh chan<- Message
}

const virtualActorInactivity = 2 * time.Second

// NewVirtualActorManager creates a new actor manager
func NewVirtualActorManager() *VirtualActorManager {
	return &VirtualActorManager{
		Actors:   make(map[string]*virtualActorEntry),
		registry: &registry{},
	}
}

// RegisterActor adds or updates an actor function by name (via manager only).
func (vam *VirtualActorManager) RegisterActor(name string, fn ActorFunc) {
	vam.registry.register(name, fn)
}

// SetStorage sets the persistence backend for the manager.
func (vam *VirtualActorManager) SetStorage(s ActorStorage) {
	vam.storage = s
}

// CallActor sends a message to an actor (by name), activating it as needed.
func (vam *VirtualActorManager) CallActor(ctx context.Context, actorName string, input interface{}, outputCh chan<- Message) {
	vam.mu.Lock()
	entry, ok := vam.Actors[actorName]
	if !ok {
		// Actor activation: load state if persistence enabled
		var loadedState interface{}
		if vam.storage != nil {
			loadedState, _ = vam.storage.Load(ctx, actorName)
		}
		entry = &virtualActorEntry{
			name:     actorName,
			msgCh:    make(chan actorMsg, 10),
			quitCh:   make(chan struct{}),
			lastUsed: time.Now(),
			state:    loadedState,
			registry: vam.registry, // propagate parent registry
		}
		vam.Actors[actorName] = entry
		go entry.actorLoop(vam)
	}
	entry.lastUsed = time.Now()
	vam.mu.Unlock()
	entry.msgCh <- actorMsg{ctx: ctx, input: input, outputCh: outputCh}
}

func (a *virtualActorEntry) actorLoop(vam *VirtualActorManager) {
	inactivityTimer := time.NewTimer(virtualActorInactivity)
	defer inactivityTimer.Stop()
	for {
		select {
		case msg := <-a.msgCh:
			fn, ok := a.registry.get(a.name)
			var result, err interface{}
			var handlerInput interface{}
			if msg.input == nil && a.state != nil {
				handlerInput = a.state // If no caller input, use persisted state
			} else {
				handlerInput = msg.input
			}
			if ok {
				result, err = fn(msg.ctx, handlerInput)
			} else {
				result, err = nil, "actor not found"
			}

			// State persistence logic: support StateResult for explicit state
			var userResult interface{}
			switch sr := result.(type) {
			case StateResult:
				a.state = sr.State
				userResult = sr.Output
			default:
				a.state = result // fallback: persist entire output
				userResult = result
			}

			msg.outputCh <- Message{ActorName: a.name, Payload: struct {
				Result interface{}
				Error  interface{}
			}{userResult, err}}
			inactivityTimer.Reset(virtualActorInactivity)
		case <-inactivityTimer.C:
			// Save state if persistence enabled
			if vam.storage != nil {
				_ = vam.storage.Save(context.Background(), a.name, a.state)
			}
			vam.mu.Lock()
			delete(vam.Actors, a.name)
			vam.mu.Unlock()
			return
		case <-a.quitCh:
			return
		}
	}
}
