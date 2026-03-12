# K-Drive Sync Model

## Overview

This document defines the sync strategy for K-Drive, including the approach decision, tradeoffs, conflict resolution strategy, and sync state management.

## Current State

K-Drive currently uses **rclone mount** with `--vfs-cache-mode writes` for file operations. This provides:

- Write-through caching where files are uploaded when closed
- Synchronous writes via VFS layer
- No explicit sync status tracking
- No conflict detection or handling

This approach works well for single-user, single-device scenarios but lacks visibility into sync state and has no conflict handling.

## Sync Approach Decision

### Decision: Hybrid Model

K-Drive will use a **hybrid model** combining mount-based access with explicit bisync operations:

1. **Mount (default)**: Users access files through mounted drives with write-through VFS caching
2. **Explicit Sync**: Users can trigger `rclone bisync` for bidirectional sync with conflict handling
3. **Scheduled Sync**: Optional periodic bisync runs for syncing changes from other devices

### Why Hybrid?

| Approach | Pros | Cons |
|----------|------|------|
| **Mount-only** | Instant access, familiar filesystem UX, no sync scheduling needed | No conflict handling, last-write-wins, no multi-device coordination |
| **Bisync-only** | Robust conflict handling, multi-device support, real files | Scheduled (not instant), requires local copy, more storage |
| **Hybrid** | Best of both: instant access + explicit sync safety | More complex UI, requires user awareness of sync modes |

The hybrid model preserves the convenience of mounted filesystem access while providing explicit sync with conflict handling when users need to coordinate across devices or ensure data consistency.

## rclone Bisync Integration

### Command Structure

```bash
rclone bisync <local-path> <remote:> [flags]
```

For K-Drive:
- Path1: Local mount path (e.g., `K:\MyDrive`)
- Path2: Remote path (e.g., `gdrive:MyDrive`)

### Key Flags for K-Drive

```
--conflict-resolve newer     # Prefer newer file on conflict
--conflict-loser num         # Rename conflict loser with number suffix
--conflict-suffix .conflict  # Suffix for conflict files
--check-access               # Verify both paths are accessible
--recover                    # Auto-recover from interruptions
--resilient                  # Allow retry after less-serious errors
```

### Conflict Resolution Strategy

K-Drive uses **newer-wins** conflict resolution:

1. **Default behavior**: When both local and remote have changed, prefer the newer file
2. **Conflict files**: The losing file is renamed with `.conflict` suffix
3. **User notification**: Conflicts are surfaced in UI for user review

#### Conflict Scenarios

| Scenario | Resolution |
|----------|------------|
| File changed on both sides, same content | No action needed |
| File changed on both sides, different content | Newer wins, loser renamed to `.conflict` |
| File deleted on one side, changed on other | Changed version wins |
| New file on both sides with same name | Newer wins, loser renamed |
| Same modtime but different content | Both kept as conflicts (no automatic resolution) |

### Safety Measures

Bisync provides built-in safety:

- **Lock file**: Prevents concurrent bisync runs
- **Max-delete check**: Aborts if >50% files would be deleted (configurable)
- **Check-access**: Verifies `RCLONE_TEST` files exist on both paths
- **Listing persistence**: Tracks prior state for accurate delta detection

## Sync State Machine

### States

```
┌─────────┐     start sync      ┌──────────┐
│  IDLE   │ ──────────────────► │ SYNCING  │
└─────────┘                     └──────────┘
     ▲                               │
     │                               │
     │         ┌─────────────────────┼─────────────────────┐
     │         │                     │                     │
     │         ▼                     ▼                     ▼
     │   ┌──────────┐         ┌──────────┐          ┌──────────┐
     └───│ SUCCESS  │         │  ERROR   │          │ CONFLICT │
         └──────────┘         └──────────┘          └──────────┘
                                   │                     │
                                   │                     │
                                   ▼                     ▼
                              ┌──────────┐         ┌──────────┐
                              │ RETRYING │         │ NEEDS    │
                              └──────────┘         │ RESOLVE  │
                                   │               └──────────┘
                                   │                     │
                                   └─────────────────────┘
```

### State Definitions

| State | Description | User Action |
|-------|-------------|-------------|
| `idle` | No active sync, last sync successful | None needed |
| `syncing` | Bisync operation in progress | Wait |
| `success` | Sync completed with no issues | None needed |
| `error` | Sync failed with recoverable error | Retry or investigate |
| `conflict` | Sync completed with conflicts | Review conflict files |
| `needs_resolve` | Manual resolution required | User intervention |
| `retrying` | Automatic retry in progress | Wait |
| `offline` | Remote not reachable | Restore connectivity |

### State Transitions

```
idle -> syncing       (user triggers sync or scheduled)
syncing -> success    (no errors, no conflicts)
syncing -> error      (transient error: network, rate limit)
syncing -> conflict   (conflicts detected and renamed)
syncing -> needs_resolve (unrecoverable conflict)
error -> retrying     (automatic retry scheduled)
retrying -> syncing   (retry attempt started)
retrying -> error     (retry limit exceeded)
success -> idle       (reset after notification)
conflict -> idle      (user acknowledged)
offline -> syncing    (connectivity restored)
```

## Sync Events

### Event Types

| Event | When | Data |
|-------|------|------|
| `sync_started` | Bisync begins | account_id, timestamp |
| `sync_progress` | During sync | files_processed, bytes_transferred |
| `sync_completed` | Sync finishes | duration, files_synced, errors |
| `conflict_detected` | Conflict found | file_path, local_modtime, remote_modtime |
| `sync_error` | Error occurred | error_code, message, recoverable |

### Event Subscription

Frontend can subscribe to sync events:

```typescript
interface SyncEvent {
  type: 'sync_started' | 'sync_progress' | 'sync_completed' | 'conflict_detected' | 'sync_error';
  accountId: string;
  timestamp: Date;
  data?: SyncEventData;
}
```

## User Experience

### Sync Triggers

1. **Manual sync**: User clicks "Sync Now" button
2. **Scheduled sync**: Periodic sync at configurable interval (default: 10 minutes)
3. **On mount**: Optional sync when mounting a drive
4. **On unmount**: Sync before unmounting to ensure consistency

### Status Display

| UI Element | Content |
|------------|---------|
| Account row icon | Sync state indicator (checkmark, spinner, warning) |
| Status text | "Synced", "Syncing...", "X conflicts", "Error: ..." |
| Detail panel | Last sync time, next scheduled sync, conflict count |
| Notification | Toast on sync completion, conflicts, errors |

### Conflict Resolution UI

When conflicts exist:
1. Show indicator on account with conflict count
2. User can expand to see conflict list
3. Each conflict shows: filename, local vs remote info, actions
4. Actions: Keep local, Keep remote, Keep both, Open both

## Implementation Phases

### Phase 1: Sync Status Infrastructure (Issue #42)
- Add SyncStateRepository to storage layer
- Define sync states and events
- Backend bindings for sync status

### Phase 2: UI Integration (Issue #43)
- Sync status indicators in UI
- Sync progress display
- Error/conflict surfacing

### Phase 3: Conflict Handling (Issue #44)
- Conflict detection integration
- Conflict persistence
- Resolution UI

### Phase 4: Recovery & Testing (Issue #45)
- Retry logic with backoff
- Offline detection
- Integration tests

## Configuration

### Account-level Sync Settings

```typescript
interface SyncConfig {
  enabled: boolean;           // Enable scheduled sync
  intervalMinutes: number;    // Sync interval (default: 10)
  conflictResolution: 'newer' | 'local' | 'remote' | 'manual';
  syncOnMount: boolean;       // Sync when mounting
  syncOnUnmount: boolean;     // Sync before unmounting
}
```

### Global Settings

```typescript
interface GlobalSyncSettings {
  maxRetries: number;         // Max retry attempts (default: 3)
  retryBackoffMs: number;     // Initial retry backoff (default: 1000)
  maxDeletePercent: number;   // Safety threshold (default: 50)
}
```

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Data loss from incorrect sync | Max-delete safety, check-access verification |
| Race conditions between mount and sync | Lock files, serialize operations |
| Conflict files accumulating | Auto-cleanup after resolution, user notification |
| Large sync operations blocking UI | Background sync with progress updates |
| Network instability during sync | Retry logic, resumable sync with `--recover` |

## References

- [rclone bisync documentation](https://rclone.org/bisync/)
- [rclone mount documentation](https://rclone.org/commands/rclone_mount/)
- [rclone VFS documentation](https://rclone.org/commands/rclone_mount/#virtual-file-system-vfs)
