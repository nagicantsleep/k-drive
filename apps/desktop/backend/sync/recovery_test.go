package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"KDrive/backend/storage"
)

func TestRecoveryManager_ScheduleRetry(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	config := RetryConfig{
		MaxRetries:  3,
		RetryBaseMs: 50,
		RetryMaxMs:  500,
	}
	rm := NewRecoveryManager(service, config)

	ctx := context.Background()
	accountID := "test-account"

	called := make(chan bool, 1)
	retryFunc := func(ctx context.Context) error {
		called <- true
		return nil
	}

	err := rm.ScheduleRetry(ctx, accountID, retryFunc)
	if err != nil {
		t.Fatalf("ScheduleRetry failed: %v", err)
	}

	// Check state was set to retrying
	status, _ := service.GetStatus(ctx, accountID)
	if status.State != storage.SyncStateRetrying {
		t.Errorf("expected state %s, got %s", storage.SyncStateRetrying, status.State)
	}

	// Wait for retry to fire
	select {
	case <-called:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("retry function was not called within timeout")
	}
}

func TestRecoveryManager_MaxRetries(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	config := RetryConfig{
		MaxRetries:  2,
		RetryBaseMs: 10,
		RetryMaxMs:  100,
	}
	rm := NewRecoveryManager(service, config)

	ctx := context.Background()
	accountID := "test-account"

	retryFunc := func(ctx context.Context) error {
		return nil
	}

	// Use up retries
	_ = rm.ScheduleRetry(ctx, accountID, retryFunc)
	_ = rm.ScheduleRetry(ctx, accountID, retryFunc)

	// Third retry should fail
	err := rm.ScheduleRetry(ctx, accountID, retryFunc)
	if err == nil {
		t.Fatal("expected error after max retries exceeded")
	}
}

func TestRecoveryManager_ResetRetries(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	config := DefaultRetryConfig()
	rm := NewRecoveryManager(service, config)

	accountID := "test-account"

	// Simulate some retries
	rm.retryCounts[accountID] = &RetryState{Attempts: 2}

	rm.ResetRetries(accountID)

	_, exists := rm.GetRetryState(accountID)
	if exists {
		t.Error("expected retry state to be cleared after reset")
	}
}

func TestRecoveryManager_CancelRetries(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	config := RetryConfig{
		MaxRetries:  3,
		RetryBaseMs: 5000,
		RetryMaxMs:  60000,
	}
	rm := NewRecoveryManager(service, config)

	ctx := context.Background()
	accountID := "test-account"

	called := make(chan bool, 1)
	retryFunc := func(ctx context.Context) error {
		called <- true
		return nil
	}

	_ = rm.ScheduleRetry(ctx, accountID, retryFunc)
	rm.CancelRetries(accountID)

	// Should not fire
	select {
	case <-called:
		t.Fatal("retry should not have been called after cancel")
	case <-time.After(200 * time.Millisecond):
		// Expected: no call
	}
}

func TestRecoveryManager_ExponentialBackoff(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	config := RetryConfig{
		MaxRetries:  5,
		RetryBaseMs: 100,
		RetryMaxMs:  10000,
	}
	rm := NewRecoveryManager(service, config)

	// Verify exponential increase
	d1 := rm.calculateBackoff(1)
	d2 := rm.calculateBackoff(2)
	d3 := rm.calculateBackoff(3)

	if d2 <= d1 {
		t.Errorf("expected d2 (%v) > d1 (%v)", d2, d1)
	}
	if d3 <= d2 {
		t.Errorf("expected d3 (%v) > d2 (%v)", d3, d2)
	}

	// Verify max cap
	dMax := rm.calculateBackoff(20)
	if dMax > time.Duration(config.RetryMaxMs)*time.Millisecond {
		t.Errorf("expected max backoff <= %dms, got %v", config.RetryMaxMs, dMax)
	}
}

func TestRecoveryManager_RetryWithFailure(t *testing.T) {
	syncStateRepo := newMockSyncStateRepository()
	conflictRepo := newMockSyncConflictRepository()
	service := NewService(syncStateRepo, conflictRepo)

	config := RetryConfig{
		MaxRetries:  2,
		RetryBaseMs: 50,
		RetryMaxMs:  200,
	}
	rm := NewRecoveryManager(service, config)

	ctx := context.Background()
	accountID := "retry-fail-account"

	callCount := 0
	retryFunc := func(ctx context.Context) error {
		callCount++
		return errors.New("still failing")
	}

	err := rm.ScheduleRetry(ctx, accountID, retryFunc)
	if err != nil {
		t.Fatalf("initial schedule should not fail: %v", err)
	}

	// Wait for retry cascade to complete
	time.Sleep(1 * time.Second)

	if callCount < 1 {
		t.Errorf("expected at least 1 retry call, got %d", callCount)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()
	if config.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", config.MaxRetries)
	}
	if config.RetryBaseMs != 1000 {
		t.Errorf("expected RetryBaseMs 1000, got %d", config.RetryBaseMs)
	}
	if config.RetryMaxMs != 60000 {
		t.Errorf("expected RetryMaxMs 60000, got %d", config.RetryMaxMs)
	}
}
