# K-Drive Architecture (MVP Baseline)

## Scope (MVP)
- Platform priority: Windows first, macOS second.
- Provider priority: S3-compatible first, then Google Drive and OneDrive.
- One mounted virtual drive per connected remote account.

## System Overview
- **Desktop shell:** Wails app (`apps/desktop`) with Go backend + React/TypeScript frontend.
- **Storage/mount engine:** `rclone` process per account.
- **Mount drivers:** WinFsp (Windows), macFUSE (macOS).
- **Data persistence:** SQLite for account metadata and app state.
- **Secret storage:** OS vault only (Windows DPAPI / macOS Keychain).

## Module Boundaries

### Frontend (`apps/desktop/frontend`)
- Account onboarding flows (OAuth and credentials forms).
- Drive list and mount status dashboard.
- Mount actions (mount/unmount/retry) via Wails bindings.

### Backend (`apps/desktop/backend`)

#### `connectors/`
- Provider adapters: S3-compatible, Google Drive, OneDrive.
- Normalizes provider-specific setup into shared DTOs.
- Exposes capability metadata (supports OAuth, supports static credentials, required fields).

#### `auth/`
- OAuth flow orchestration (Google/Microsoft).
- Token exchange/refresh boundaries.
- Delegates secret persistence to secure store abstractions.

#### `storage/`
- SQLite repositories for non-secret entities:
  - Accounts
  - Mount profiles
  - Last known mount status/events
- Secret reference mapping only; no plaintext tokens/keys.

#### `mount/`
- Builds per-account `rclone` runtime config.
- Starts/stops one mount process per account.
- Health checks and auto-remount policy.
- Emits mount lifecycle events to UI.

## Runtime Flow
1. User adds account (OAuth or credentials).
2. Backend validates and persists metadata in SQLite.
3. Secrets are stored in OS secure vault; DB stores only references.
4. Mount manager resolves connector config and starts `rclone mount`.
5. Local virtual drive appears for that account.
6. Supervisor monitors process health and auto-recovers when needed.

## Contracts to Keep Stable
- Connector interface: provider-agnostic account config DTO in, normalized rclone remote options out.
- Mount manager interface: mount/unmount/status operations keyed by account ID.
- Storage interface: account + mount state repositories; no secrets API exposure.

## Security Baseline
- Never log secrets, tokens, or credential payloads.
- Encrypt/secure all secrets via OS-native keychain mechanisms.
- Validate provider callback and OAuth state parameter.
- Principle of least privilege for OAuth scopes and filesystem mount permissions.
