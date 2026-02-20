package actor

import (
	"context"
	"sync"
	"time"

	"github.com/rickKoch/opsflow/persistence"
)

// persistentMailbox stores messages in memory and persists via Persistence on every change.
type persistentMailbox struct {
	mu   sync.Mutex
	msgs []Message
	pers persistence.Persistence
	id   persistence.PID
	// write-behind settings
	flushInterval  time.Duration
	flushThreshold int
	dirty          bool
	flushCh        chan struct{}
	stopCh         chan struct{}
}

func newPersistentMailbox(p persistence.Persistence, id persistence.PID, _size int) *persistentMailbox {
	pm := &persistentMailbox{pers: p, id: id, flushInterval: 100 * time.Millisecond, flushThreshold: 10, flushCh: make(chan struct{}, 1), stopCh: make(chan struct{})}
	if p != nil {
		if loaded, err := p.LoadMailbox(context.Background(), id); err == nil && len(loaded) > 0 {
			for _, b := range loaded {
				pm.msgs = append(pm.msgs, Message{Payload: b})
			}
		}
	}
	// start background flusher
	go pm.flusher()
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
	return m.pers.SaveMailbox(context.Background(), m.id, out)
}

func (m *persistentMailbox) Enqueue(msg Message) {
	m.mu.Lock()
	m.msgs = append(m.msgs, msg)
	m.dirty = true
	// signal flush if threshold reached
	if len(m.msgs) >= m.flushThreshold {
		select {
		case m.flushCh <- struct{}{}:
		default:
		}
	}
	m.mu.Unlock()
}

func (m *persistentMailbox) Dequeue() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.msgs) == 0 {
		return Message{}, false
	}
	msg := m.msgs[0]
	m.msgs = m.msgs[1:]
	m.dirty = true
	if len(m.msgs) >= m.flushThreshold {
		select {
		case m.flushCh <- struct{}{}:
		default:
		}
	}
	return msg, true
}

func (m *persistentMailbox) Drain() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Message, len(m.msgs))
	copy(out, m.msgs)
	return out
}

// flusher runs in background and writes mailbox to persistence when dirty.
func (m *persistentMailbox) flusher() {
	ticker := time.NewTicker(m.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			// final flush
			m.mu.Lock()
			if m.dirty {
				_ = m.persistLocked()
				m.dirty = false
			}
			m.mu.Unlock()
			return
		case <-m.flushCh:
			m.mu.Lock()
			if m.dirty {
				_ = m.persistLocked()
				m.dirty = false
			}
			m.mu.Unlock()
		case <-ticker.C:
			m.mu.Lock()
			if m.dirty {
				_ = m.persistLocked()
				m.dirty = false
			}
			m.mu.Unlock()
		}
	}
}

// Close stops background flusher and persists messages.
func (m *persistentMailbox) Close() {
	close(m.stopCh)
}
