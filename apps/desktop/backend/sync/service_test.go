package sync

import (
	"context"
	"testing"
	"time"

	"KDrive/backend/storage"
)

// mockSyncStateRepository is a mock implementation for testing
type mockSyncStateRepository struct {
	statuses map[string]storage.SyncStatus
}

func newMockSyncStateRepository() *mockSyncStateRepository {
	return &mockSyncStateRepository{
		statuses: make(map[string]storage.SyncStatus),
	}
}

func (m *mockSyncStateRepository) UpsertSyncStatus(ctx context.Context, status storage.SyncStatus) error {
	m.statuses[status.AccountID] = status
	return nil
}

func (m *mockSyncStateRepository) GetSyncStatus(ctx context.Context, accountID string) (storage.SyncStatus, error) {
	status, ok := m.statuses[accountID]
	if !ok {
		return storage.SyncStatus{}, storage.ErrSyncStateNotFound
	}
	return status, nil
}

func (m *mockSyncStateRepository) ListSyncStatuses(ctx context.Context) ([]storage.SyncStatus, error) {
	result := make([]storage.SyncStatus, 0, len(m.statuses))
	for _, status := range m.statuses {
		result = append(result, status)
	}
	return result, nil
}

// mockSyncConflictRepository is a mock implementation for testing
type mockSyncConflictRepository struct {
	conflicts map[string]storage.SyncConflict
}

func newMockSyncConflictRepository() *mockSyncConflictRepository {
	return &mockSyncConflictRepository{
		conflicts: make(map[string]storage.SyncConflict),
	}
}

func (m *mockSyncConflictRepository) SaveConflict(ctx context.Context, conflict storage.SyncConflict) error {
	m.conflicts[conflict.ID] = conflict
	return nil
}

func (m *mockSyncConflictRepository) ListConflicts(ctx context.Context, accountID string) ([]storage.SyncConflict, error) {
	result := make([]storage.SyncConflict, 0)
	for _, conflict := range m.conflicts {
		if conflict.AccountID == accountID {
			result = append(result, conflict)
		}
	}
	return result, nil
}

func (m *mockSyncConflictRepository) DeleteConflict(ctx context.Context, conflictID string) error {
	delete(m.conflicts, conflictID)
	return nil
}

func TestService_StartSync(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	ctx := context.Background()
	accountID := "test-account"

	// Track events with a channel for async testing
	eventChan := make(chan Event, 5)
	service.OnEvent(func(e Event) {
		eventChan <- e
	})

	err := service.StartSync(ctx, accountID)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Check state
	status, err := service.GetStatus(ctx, accountID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.State != storage.SyncStateSyncing {
		t.Errorf("expected state %s, got %s", storage.SyncStateSyncing, status.State)
	}

	// Wait for sync_started event (may receive state_changed first)
	timeout := time.After(2 * time.Second)
	found := false
	for !found {
		select {
		case receivedEvent := <-eventChan:
			if receivedEvent.Type == EventTypeSyncStarted {
				found = true
			}
		case <-timeout:
			t.Fatal("expected sync_started event to be emitted within 2 seconds")
		}
	}
}

func TestService_CompleteSync(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	ctx := context.Background()
	accountID := "test-account"

	err := service.CompleteSync(ctx, accountID, 10, 1024, time.Second)
	if err != nil {
		t.Fatalf("CompleteSync failed: %v", err)
	}

	status, err := service.GetStatus(ctx, accountID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.State != storage.SyncStateSuccess {
		t.Errorf("expected state %s, got %s", storage.SyncStateSuccess, status.State)
	}
	if status.FilesSynced != 10 {
		t.Errorf("expected FilesSynced 10, got %d", status.FilesSynced)
	}
	if status.BytesTransferred != 1024 {
		t.Errorf("expected BytesTransferred 1024, got %d", status.BytesTransferred)
	}
	if status.LastSyncAt.IsZero() {
		t.Error("expected LastSyncAt to be set")
	}
}

func TestService_FailSync(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	ctx := context.Background()
	accountID := "test-account"

	testErr := storage.ErrSyncStateNotFound
	err := service.FailSync(ctx, accountID, testErr, true)
	if err != nil {
		t.Fatalf("FailSync failed: %v", err)
	}

	status, err := service.GetStatus(ctx, accountID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.State != storage.SyncStateRetrying {
		t.Errorf("expected state %s, got %s", storage.SyncStateRetrying, status.State)
	}
	if status.LastError == "" {
		t.Error("expected LastError to be set")
	}
}

func TestService_RecordConflict(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	ctx := context.Background()
	accountID := "test-account"

	err := service.RecordConflict(ctx, accountID, "/path/to/file.txt", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("RecordConflict failed: %v", err)
	}

	status, err := service.GetStatus(ctx, accountID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.State != storage.SyncStateConflict {
		t.Errorf("expected state %s, got %s", storage.SyncStateConflict, status.State)
	}
	if status.ConflictCount != 1 {
		t.Errorf("expected ConflictCount 1, got %d", status.ConflictCount)
	}

	// Check conflict was saved
	conflicts, err := service.ListConflicts(ctx, accountID)
	if err != nil {
		t.Fatalf("ListConflicts failed: %v", err)
	}
	if len(conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts))
	}
}

func TestService_ResolveConflict(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	ctx := context.Background()
	accountID := "test-account"
	conflictID := accountID + ":/path/to/file.txt"

	// First record a conflict
	err := service.RecordConflict(ctx, accountID, "/path/to/file.txt", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("RecordConflict failed: %v", err)
	}

	// Resolve it
	err = service.ResolveConflict(ctx, accountID, conflictID)
	if err != nil {
		t.Fatalf("ResolveConflict failed: %v", err)
	}

	// Check conflict count
	status, err := service.GetStatus(ctx, accountID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ConflictCount != 0 {
		t.Errorf("expected ConflictCount 0, got %d", status.ConflictCount)
	}
	if status.State != storage.SyncStateSuccess {
		t.Errorf("expected state %s after conflict resolution, got %s", storage.SyncStateSuccess, status.State)
	}
}

func TestService_StateTransitions(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	ctx := context.Background()
	accountID := "test-account"

	// Track all state changes with a buffered channel
	eventChan := make(chan Event, 10)
	service.OnEvent(func(e Event) {
		if e.Type == EventTypeStateChanged {
			eventChan <- e
		}
	})

	// Transition through states
	_ = service.TransitionState(ctx, accountID, storage.SyncStateSyncing)
	_ = service.TransitionState(ctx, accountID, storage.SyncStateSuccess)
	_ = service.TransitionState(ctx, accountID, storage.SyncStateSyncing)
	_ = service.TransitionState(ctx, accountID, storage.SyncStateError)

	// Wait for all events with timeout
	var stateChanges []storage.SyncState
	timeout := time.After(2 * time.Second)
	for len(stateChanges) < 4 {
		select {
		case e := <-eventChan:
			stateChanges = append(stateChanges, e.Data.State)
		case <-timeout:
			t.Fatalf("expected 4 state changes, got %d", len(stateChanges))
		}
	}

	if len(stateChanges) != 4 {
		t.Errorf("expected 4 state changes, got %d", len(stateChanges))
	}
}

func TestService_GetStatus_NotFound(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	ctx := context.Background()
	accountID := "nonexistent-account"

	status, err := service.GetStatus(ctx, accountID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	// Should return idle state for non-existent account
	if status.State != storage.SyncStateIdle {
		t.Errorf("expected state %s for non-existent account, got %s", storage.SyncStateIdle, status.State)
	}
}
