package sync

import (
	"context"
	"errors"
	"sync"
	"time"

	"KDrive/backend/storage"
)

// EventType represents the type of sync event
type EventType string

const (
	EventTypeSyncStarted    EventType = "sync_started"
	EventTypeSyncProgress   EventType = "sync_progress"
	EventTypeSyncCompleted  EventType = "sync_completed"
	EventTypeConflictFound  EventType = "conflict_detected"
	EventTypeSyncError      EventType = "sync_error"
	EventTypeStateChanged   EventType = "state_changed"
)

// Event represents a sync event
type Event struct {
	Type      EventType
	AccountID string
	Timestamp time.Time
	Data      EventData
}

// EventData contains event-specific data
type EventData struct {
	State           storage.SyncState `json:"state,omitempty"`
	FilesProcessed  int               `json:"filesProcessed,omitempty"`
	BytesTransferred int64             `json:"bytesTransferred,omitempty"`
	FilesSynced     int               `json:"filesSynced,omitempty"`
	Duration        time.Duration     `json:"duration,omitempty"`
	Error           error             `json:"error,omitempty"`
	ConflictPath    string            `json:"conflictPath,omitempty"`
	LocalModTime    time.Time         `json:"localModTime,omitempty"`
	RemoteModTime   time.Time         `json:"remoteModTime,omitempty"`
	Recoverable     bool              `json:"recoverable,omitempty"`
}

// EventHandler is a callback for sync events
type EventHandler func(Event)

// Service manages sync state and emits events
type Service struct {
	syncStateRepo   storage.SyncStateRepository
	conflictRepo    storage.SyncConflictRepository
	handlers        []EventHandler
	handlersMu      sync.RWMutex
	stateTransitions map[string]storage.SyncState // account_id -> current state
	transitionsMu   sync.RWMutex
}

// NewService creates a new sync service
func NewService(syncStateRepo storage.SyncStateRepository, conflictRepo storage.SyncConflictRepository) *Service {
	return &Service{
		syncStateRepo:    syncStateRepo,
		conflictRepo:     conflictRepo,
		handlers:         make([]EventHandler, 0),
		stateTransitions: make(map[string]storage.SyncState),
	}
}

// OnEvent registers an event handler
func (s *Service) OnEvent(handler EventHandler) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()
	s.handlers = append(s.handlers, handler)
}

// emit sends an event to all registered handlers
func (s *Service) emit(event Event) {
	s.handlersMu.RLock()
	handlers := make([]EventHandler, len(s.handlers))
	copy(handlers, s.handlers)
	s.handlersMu.RUnlock()

	for _, h := range handlers {
		go h(event)
	}
}

// TransitionState changes the sync state for an account
func (s *Service) TransitionState(ctx context.Context, accountID string, newState storage.SyncState) error {
	s.transitionsMu.Lock()
	oldState, exists := s.stateTransitions[accountID]
	s.stateTransitions[accountID] = newState
	s.transitionsMu.Unlock()

	now := time.Now().UTC()

	// Get existing status to preserve fields
	status, err := s.syncStateRepo.GetSyncStatus(ctx, accountID)
	if err != nil && !errors.Is(err, storage.ErrSyncStateNotFound) {
		return err
	}

	// Update state
	status.AccountID = accountID
	status.State = newState
	status.UpdatedAt = now

	// Reset error on success/idle
	if newState == storage.SyncStateSuccess || newState == storage.SyncStateIdle {
		status.LastError = ""
	}

	// Persist the new state
	if err := s.syncStateRepo.UpsertSyncStatus(ctx, status); err != nil {
		return err
	}

	// Emit state change event if state actually changed
	if !exists || oldState != newState {
		s.emit(Event{
			Type:      EventTypeStateChanged,
			AccountID: accountID,
			Timestamp: now,
			Data: EventData{
				State: newState,
			},
		})
	}

	return nil
}

// StartSync initiates a sync operation
func (s *Service) StartSync(ctx context.Context, accountID string) error {
	if err := s.TransitionState(ctx, accountID, storage.SyncStateSyncing); err != nil {
		return err
	}

	s.emit(Event{
		Type:      EventTypeSyncStarted,
		AccountID: accountID,
		Timestamp: time.Now().UTC(),
	})

	return nil
}

// CompleteSync marks a sync as completed successfully
func (s *Service) CompleteSync(ctx context.Context, accountID string, filesSynced int, bytesTransferred int64, duration time.Duration) error {
	now := time.Now().UTC()

	status := storage.SyncStatus{
		AccountID:        accountID,
		State:            storage.SyncStateSuccess,
		LastSyncAt:       now,
		FilesSynced:      filesSynced,
		BytesTransferred: bytesTransferred,
		UpdatedAt:        now,
	}

	if err := s.syncStateRepo.UpsertSyncStatus(ctx, status); err != nil {
		return err
	}

	s.transitionsMu.Lock()
	s.stateTransitions[accountID] = storage.SyncStateSuccess
	s.transitionsMu.Unlock()

	s.emit(Event{
		Type:      EventTypeSyncCompleted,
		AccountID: accountID,
		Timestamp: now,
		Data: EventData{
			FilesSynced:      filesSynced,
			BytesTransferred: bytesTransferred,
			Duration:         duration,
		},
	})

	return nil
}

// FailSync marks a sync as failed
func (s *Service) FailSync(ctx context.Context, accountID string, syncErr error, recoverable bool) error {
	state := storage.SyncStateError
	if recoverable {
		state = storage.SyncStateRetrying
	}

	status := storage.SyncStatus{
		AccountID: accountID,
		State:     state,
		LastError: syncErr.Error(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := s.syncStateRepo.UpsertSyncStatus(ctx, status); err != nil {
		return err
	}

	s.transitionsMu.Lock()
	s.stateTransitions[accountID] = state
	s.transitionsMu.Unlock()

	s.emit(Event{
		Type:      EventTypeSyncError,
		AccountID: accountID,
		Timestamp: time.Now().UTC(),
		Data: EventData{
			Error:       syncErr,
			Recoverable: recoverable,
		},
	})

	return nil
}

// RecordConflict records a sync conflict
func (s *Service) RecordConflict(ctx context.Context, accountID, filePath string, localModTime, remoteModTime time.Time) error {
	conflictID := generateConflictID(accountID, filePath)

	conflict := storage.SyncConflict{
		ID:            conflictID,
		AccountID:     accountID,
		FilePath:      filePath,
		LocalModTime:  localModTime,
		RemoteModTime: remoteModTime,
		CreatedAt:     time.Now().UTC(),
	}

	if err := s.conflictRepo.SaveConflict(ctx, conflict); err != nil {
		return err
	}

	// Update conflict count
	status, err := s.syncStateRepo.GetSyncStatus(ctx, accountID)
	if err != nil && !errors.Is(err, storage.ErrSyncStateNotFound) {
		return err
	}

	status.AccountID = accountID
	status.State = storage.SyncStateConflict
	status.ConflictCount++
	status.UpdatedAt = time.Now().UTC()

	if err := s.syncStateRepo.UpsertSyncStatus(ctx, status); err != nil {
		return err
	}

	s.transitionsMu.Lock()
	s.stateTransitions[accountID] = storage.SyncStateConflict
	s.transitionsMu.Unlock()

	s.emit(Event{
		Type:      EventTypeConflictFound,
		AccountID: accountID,
		Timestamp: time.Now().UTC(),
		Data: EventData{
			ConflictPath:  filePath,
			LocalModTime:  localModTime,
			RemoteModTime: remoteModTime,
		},
	})

	return nil
}

// GetStatus returns the current sync status for an account
func (s *Service) GetStatus(ctx context.Context, accountID string) (storage.SyncStatus, error) {
	status, err := s.syncStateRepo.GetSyncStatus(ctx, accountID)
	if err != nil {
		if errors.Is(err, storage.ErrSyncStateNotFound) {
			return storage.SyncStatus{
				AccountID: accountID,
				State:     storage.SyncStateIdle,
			}, nil
		}
		return storage.SyncStatus{}, err
	}
	return status, nil
}

// ListStatuses returns sync statuses for all accounts
func (s *Service) ListStatuses(ctx context.Context) ([]storage.SyncStatus, error) {
	return s.syncStateRepo.ListSyncStatuses(ctx)
}

// ResolveConflict marks a conflict as resolved
func (s *Service) ResolveConflict(ctx context.Context, accountID, conflictID string) error {
	if err := s.conflictRepo.DeleteConflict(ctx, conflictID); err != nil {
		return err
	}

	// Update conflict count
	status, err := s.syncStateRepo.GetSyncStatus(ctx, accountID)
	if err != nil {
		return err
	}

	if status.ConflictCount > 0 {
		status.ConflictCount--
	}

	if status.ConflictCount == 0 {
		status.State = storage.SyncStateSuccess
	}
	status.UpdatedAt = time.Now().UTC()

	return s.syncStateRepo.UpsertSyncStatus(ctx, status)
}

// ListConflicts returns all conflicts for an account
func (s *Service) ListConflicts(ctx context.Context, accountID string) ([]storage.SyncConflict, error) {
	return s.conflictRepo.ListConflicts(ctx, accountID)
}

// SetOffline marks an account as offline
func (s *Service) SetOffline(ctx context.Context, accountID string) error {
	return s.TransitionState(ctx, accountID, storage.SyncStateOffline)
}

func generateConflictID(accountID, filePath string) string {
	return accountID + ":" + filePath
}
