# K-Drive Implementation Checklist

## As Of

March 11, 2026

## Goal 1: Connect To Remote Drive

### S3-Compatible

- [x] Backend connector exists for S3 account validation and rclone remote config generation.
- [x] Wails backend exposes S3 account creation through `CreateS3Account`.
- [x] Account metadata is persisted in SQLite.
- [x] S3 access key and secret key are stored outside account metadata.
- [x] Windows secret protection uses DPAPI.
- [x] Frontend form supports adding S3 accounts.
- [x] Provider-neutral account creation flow exists.
- [x] Provider capability metadata is exposed to the frontend.

### Google Drive

- [ ] Google Drive connector implementation exists.
- [x] Google OAuth flow exists beyond placeholder wiring.
- [ ] Google account onboarding is exposed in the UI.

### OneDrive

- [ ] OneDrive connector implementation exists.
- [ ] Microsoft OAuth flow exists beyond placeholder wiring.
- [ ] OneDrive account onboarding is exposed in the UI.

## Goal 2: Mount Remote Drive To Local Machine As Virtual Drive

### Windows

- [x] rclone config file is generated and updated per account.
- [x] `rclone mount` process is started per mounted account.
- [x] Mount state is tracked as `stopped`, `mounting`, `mounted`, or `failed`.
- [x] Mount preflight checks verify `rclone` availability.
- [x] Mount preflight checks verify WinFsp availability.
- [x] Mount preflight checks verify mount base directory writability.
- [x] Mount failure categories are surfaced to the UI.
- [x] Retry flow exists for unexpected mount process failures.
- [x] Auto-remount on app startup exists for accounts previously marked mounted.
- [x] Frontend can mount, unmount, and retry.
- [ ] User-selectable drive letter or custom mount path is supported.
- [ ] Rich mount health telemetry exists beyond process-state tracking.

### macOS

- [ ] macOS secure secret storage is implemented.
- [ ] macOS mount dependency checks exist for macFUSE.
- [ ] macOS mount path and runtime behavior are validated.
- [ ] macOS onboarding and mount flow are covered by tests.

## Goal 3: Sync Between Local Virtual Drive And Remote Drive

- [x] Mounted filesystem is opened through `rclone mount`.
- [x] Mount uses `--vfs-cache-mode writes`, enabling write-through behavior for the mounted drive.
- [ ] Explicit sync engine or sync job exists.
- [ ] Background sync status is surfaced in the UI.
- [ ] Conflict handling and recovery flows exist.
- [ ] Offline queueing or deferred reconciliation exists.
- [ ] Separate `rclone sync` or `rclone bisync` workflow exists.

## Current Summary

### Implemented

- Windows-first S3 account onboarding
- SQLite persistence for accounts and mount state
- Windows DPAPI-backed secret protection
- rclone-backed mount orchestration
- Mount retry, restart recovery, and error classification
- Basic React UI for S3 account creation and mount actions
- Backend tests for the S3 and mount lifecycle
- Provider-neutral onboarding contracts and connector capability metadata
- Generic `CreateAccount` binding replacing S3-specific API
- OAuth session and callback foundation (PKCE, loopback listener, token store)
- Frontend provider selection and dynamic field rendering from capability metadata
- OAuth-scheme frontend branching for Google and Microsoft providers

### Still In Progress

- Google Drive and OneDrive connector implementations
- macOS secret storage and mount support
- Explicit sync product workflows
- Frontend automated test and lint setup

## Notes

- The current product is best described as a working Windows S3 mount MVP.
- Sync is currently implicit through a writable mounted filesystem, not a dedicated sync subsystem.
