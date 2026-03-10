# K-Drive Epic Roadmap

## As Of

March 11, 2026

## Scope

This roadmap lists the epics necessary to reach the current product goals:

1. Connect to remote drives, starting with S3 and later Google Drive and OneDrive.
2. Mount remote drives to the local machine as virtual drives, starting with Windows and later macOS.
3. Support reliable in/out file flow between the mounted local drive and the remote drive.

Completed and merged epics are included and left as completed.

## Epic Status Key

- `Completed`: Code is already merged into `master`.
- `Planned`: Required next epic, not yet delivered.
- `Later`: Necessary, but lower priority than the current next epic.

## Completed Epics

### 1. `epic:mvp-foundation`

- Status: `Completed`
- Why it was needed: Establish the desktop app skeleton, backend module layout, and baseline architecture.
- Main outcomes:
  - Wails desktop app scaffold
  - Baseline architecture and module boundaries
  - Early backend service skeletons

### 2. `epic:s3-real-mount-foundation`

- Status: `Completed`
- Why it was needed: Replace placeholder mount behavior with real local persistence, config, and process orchestration primitives.
- Main outcomes:
  - SQLite-backed account and mount-state persistence
  - Windows secure secret storage via DPAPI
  - rclone config file generation
  - Process-backed mount manager

### 3. `epic:s3-vertical-slice`

- Status: `Completed`
- Why it was needed: Deliver the first end-to-end provider flow for the product.
- Main outcomes:
  - S3 account onboarding path
  - S3 validation and normalized remote config
  - Frontend bindings for account and mount actions
  - Basic mount status UI

### 4. `epic:s3-operational-hardening`

- Status: `Completed`
- Why it was needed: Make the Windows S3 mount experience operationally usable instead of only technically functional.
- Main outcomes:
  - Dependency preflight for `rclone`, WinFsp, and mount path readiness
  - Mount state reconciliation and startup recovery
  - Failure classification and clearer diagnostics
  - Retry and recover flows in the UI
  - Lifecycle and smoke coverage for the S3 path
- Note:
  - Some GitHub issue bookkeeping for this epic is still open, but the code is already merged in `master`.

### 5. `epic:provider-expansion-groundwork`

- Status: `Completed`
- Why it was needed: The app surface was still deeply S3-specific and needed provider-neutral foundations before adding Google Drive and OneDrive.
- Main outcomes:
  - Replaced `CreateS3Account` with generic `CreateAccount` and `CreateOAuthAccount` bindings
  - Connector capability metadata (`ProviderCapability`, `CapabilityField`, `AuthScheme`) exposed to frontend
  - Generalized secret handling driven by `SecretKeys()` connector metadata
  - OAuth session and callback foundation: PKCE helpers, loopback listener, `SecretBacked` token store
  - Frontend provider selection and dynamic field rendering from capability metadata
  - Frontend branching on `authScheme` for OAuth vs static providers

## Required Next Epics

### 6. `epic:google-drive-vertical-slice`

- Status: `Later`
- Why it is necessary:
  - Google Drive is the most likely next non-S3 provider and validates the OAuth-based provider path.
- Main scope:
  - Google connector implementation
  - OAuth-based account onboarding
  - rclone config generation for Google Drive
  - End-to-end mount flow on Windows
  - Regression coverage for the new provider

### 7. `epic:explicit-sync-foundation`

- Status: `Later`
- Why it is necessary:
  - Today the product relies on writable `rclone mount`, but it does not yet expose sync as a first-class feature.
- Main scope:
  - Define the product sync model clearly
  - Add explicit sync status and health visibility
  - Add conflict and recovery behavior
  - Define whether sync uses mount-only behavior, `rclone sync`, `rclone bisync`, or a hybrid model
  - Add verification for upload, download, change propagation, and failure recovery

### 8. `epic:windows-drive-experience-hardening`

- Status: `Later`
- Why it is necessary:
  - The current Windows mount flow is functional, but still basic from an end-user drive experience perspective.
- Main scope:
  - Support user-selectable drive letter or configurable mount location
  - Improve mount UX and diagnostics
  - Tighten dependency guidance and first-run setup
  - Validate mount behavior across common Windows environments

## Lower-Priority But Necessary Epics

### 9. `epic:onedrive-vertical-slice`

- Status: `Later`
- Why it is necessary:
  - OneDrive is part of the stated provider goals, but lower priority than S3 and Google Drive.
- Main scope:
  - Microsoft OAuth flow
  - OneDrive connector implementation
  - End-to-end Windows mount flow
  - Provider-specific validation and regression coverage

### 10. `epic:macos-platform-support`

- Status: `Later`
- Why it is necessary:
  - macOS support is part of the platform goals, but not required before the Windows-first roadmap is proven.
- Main scope:
  - macOS secure secret storage
  - macFUSE dependency detection and setup guidance
  - macOS mount path and lifecycle behavior
  - Platform-specific packaging and verification

## Suggested Execution Order

1. `epic:mvp-foundation` (`Completed`)
2. `epic:s3-real-mount-foundation` (`Completed`)
3. `epic:s3-vertical-slice` (`Completed`)
4. `epic:s3-operational-hardening` (`Completed`)
5. `epic:provider-expansion-groundwork` (`Completed`)
6. `epic:google-drive-vertical-slice`
7. `epic:explicit-sync-foundation`
8. `epic:windows-drive-experience-hardening`
9. `epic:onedrive-vertical-slice`
10. `epic:macos-platform-support`

## Rationale For This Order

- The Windows S3 MVP is already in place and has been operationally hardened.
- The biggest blocker is not mount reliability anymore; it is the S3-specific app and API shape.
- Google Drive should come after provider-neutral groundwork, not before it.
- Explicit sync should be made first-class after the provider model is generalized and a second provider path exists.
- OneDrive and macOS remain necessary, but both are lower priority than the groundwork and Google Drive path.
