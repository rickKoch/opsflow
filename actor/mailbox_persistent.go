package actor

import (
	"sync"

	"github.com/rickKoch/opsflow/persistence"
)

// persistentMailbox stores messages in memory and persists via Persistence on every change.
type persistentMailbox struct {
	mu   sync.Mutex
	msgs []Message
	pers persistence.Persistence
	id   persistence.PID
}

func newPersistentMailbox(p persistence.Persistence, id persistence.PID, _size int) *persistentMailbox {
	pm := &persistentMailbox{pers: p, id: id}
	if p != nil {
		if loaded, err := p.LoadMailbox(nil, id); err == nil && len(loaded) > 0 {
			for _, b := range loaded {
				pm.msgs = append(pm.msgs, Message{Payload: b})
			}
		}
	}
	return pm
}

func (m *persistentMailbox) persistLocked() error {
	if m.pers == nil {
		return nil
	}
	out := make([][]byte, 0, len(m.msgs))
	for _, mm := range m.msgs {
		out = append(out, mm.Payload)
	}
	return m.pers.SaveMailbox(nil, m.id, out)
}

func (m *persistentMailbox) Enqueue(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
	_ = m.persistLocked()
}

func (m *persistentMailbox) Dequeue() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.msgs) == 0 {
		return Message{}, false
	}
	msg := m.msgs[0]
	m.msgs = m.msgs[1:]
	_ = m.persistLocked()
	return msg, true
}

func (m *persistentMailbox) Drain() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Message, len(m.msgs))
	copy(out, m.msgs)
	return out
}
