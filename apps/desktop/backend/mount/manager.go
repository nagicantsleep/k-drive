package mount

import (
	"context"
	"sync"
)

type State string

const (
	StateStopped State = "stopped"
	StateMounted State = "mounted"
)

type Status struct {
	AccountID string
	State     State
}

type Manager interface {
	Mount(ctx context.Context, accountID string) error
	Unmount(ctx context.Context, accountID string) error
	Status(ctx context.Context, accountID string) (Status, error)
}

type InMemoryManager struct {
	mu     sync.RWMutex
	states map[string]State
}

func NewManager() *InMemoryManager {
	return &InMemoryManager{states: make(map[string]State)}
}

func (m *InMemoryManager) Mount(_ context.Context, accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[accountID] = StateMounted
	return nil
}

func (m *InMemoryManager) Unmount(_ context.Context, accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[accountID] = StateStopped
	return nil
}

func (m *InMemoryManager) Status(_ context.Context, accountID string) (Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.states[accountID]
	if !ok {
		state = StateStopped
	}

	return Status{AccountID: accountID, State: state}, nil
}
