package sync

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"KDrive/backend/storage"
)

const (
	defaultMaxRetries    = 3
	defaultRetryBaseMs   = 1000
	defaultRetryMaxMs    = 60000
	defaultCheckInterval = 30 * time.Second
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxRetries   int
	RetryBaseMs  int
	RetryMaxMs   int
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  defaultMaxRetries,
		RetryBaseMs: defaultRetryBaseMs,
		RetryMaxMs:  defaultRetryMaxMs,
	}
}

// RetryState tracks retry state for an account
type RetryState struct {
	Attempts    int
	LastAttempt time.Time
	NextRetryAt time.Time
}

// RecoveryManager handles sync failure recovery with exponential backoff
type RecoveryManager struct {
	service     *Service
	retryConfig RetryConfig
	retryCounts map[string]*RetryState
	mu          sync.Mutex
	cancelFuncs map[string]context.CancelFunc
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(service *Service, config RetryConfig) *RecoveryManager {
	return &RecoveryManager{
		service:     service,
		retryConfig: config,
		retryCounts: make(map[string]*RetryState),
		cancelFuncs: make(map[string]context.CancelFunc),
	}
}

// ScheduleRetry schedules a retry for a failed sync
func (rm *RecoveryManager) ScheduleRetry(ctx context.Context, accountID string, retryFunc func(ctx context.Context) error) error {
	rm.mu.Lock()
	state, exists := rm.retryCounts[accountID]
	if !exists {
		state = &RetryState{}
		rm.retryCounts[accountID] = state
	}

	state.Attempts++
	attempt := state.Attempts

	if attempt > rm.retryConfig.MaxRetries {
		rm.mu.Unlock()
		return fmt.Errorf("max retries (%d) exceeded for account %s", rm.retryConfig.MaxRetries, accountID)
	}

	delay := rm.calculateBackoff(attempt)
	state.LastAttempt = time.Now()
	state.NextRetryAt = time.Now().Add(delay)

	// Cancel any existing retry for this account
	if cancel, ok := rm.cancelFuncs[accountID]; ok {
		cancel()
	}

	retryCtx, cancel := context.WithCancel(ctx)
	rm.cancelFuncs[accountID] = cancel
	rm.mu.Unlock()

	// Mark as retrying
	_ = rm.service.TransitionState(ctx, accountID, storage.SyncStateRetrying)

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
			err := retryFunc(retryCtx)
			if err != nil {
				rm.mu.Lock()
				if s, ok := rm.retryCounts[accountID]; ok && s.Attempts <= rm.retryConfig.MaxRetries {
					rm.mu.Unlock()
					_ = rm.ScheduleRetry(ctx, accountID, retryFunc)
				} else {
					rm.mu.Unlock()
					_ = rm.service.FailSync(ctx, accountID, err, false)
				}
			} else {
				rm.ResetRetries(accountID)
			}
		case <-retryCtx.Done():
			return
		}
	}()

	return nil
}

// calculateBackoff returns the delay using exponential backoff with jitter
func (rm *RecoveryManager) calculateBackoff(attempt int) time.Duration {
	baseMs := float64(rm.retryConfig.RetryBaseMs)
	maxMs := float64(rm.retryConfig.RetryMaxMs)

	delayMs := baseMs * math.Pow(2, float64(attempt-1))
	if delayMs > maxMs {
		delayMs = maxMs
	}

	return time.Duration(delayMs) * time.Millisecond
}

// ResetRetries resets the retry count for an account
func (rm *RecoveryManager) ResetRetries(accountID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.retryCounts, accountID)
	if cancel, ok := rm.cancelFuncs[accountID]; ok {
		cancel()
		delete(rm.cancelFuncs, accountID)
	}
}

// CancelRetries cancels any pending retries for an account
func (rm *RecoveryManager) CancelRetries(accountID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if cancel, ok := rm.cancelFuncs[accountID]; ok {
		cancel()
		delete(rm.cancelFuncs, accountID)
	}
}

// GetRetryState returns the retry state for an account
func (rm *RecoveryManager) GetRetryState(accountID string) (RetryState, bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	state, exists := rm.retryCounts[accountID]
	if !exists {
		return RetryState{}, false
	}
	return *state, true
}

// IsOnline checks if a remote is reachable using bisync's offline detection
func IsOnline(ctx context.Context, rclonePath, remoteName string) bool {
	runner := &BisyncRunner{rclonePath: rclonePath}
	return !runner.DetectOffline(ctx, remoteName)
}
