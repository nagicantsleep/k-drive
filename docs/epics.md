# K-Drive Epic Roadmap

> **ARCHIVED** - Epic tracking has moved to GitHub Issues. This file is kept for historical reference only.
> 
> **Active epic tracking:** Use `gh issue list --label "epic:*"` to see all epics and their status.

## Epic Issue Index

| Epic | GitHub Issue | Status |
|------|--------------|--------|
| MVP Foundation | [#1](https://github.com/nagicantsleep/k-drive/issues/1) | CLOSED |
| S3 Real Mount Foundation | [#7](https://github.com/nagicantsleep/k-drive/issues/7) | CLOSED |
| S3 Vertical Slice | [#5](https://github.com/nagicantsleep/k-drive/issues/5) | CLOSED |
| S3 Operational Hardening | [#20](https://github.com/nagicantsleep/k-drive/issues/20) | CLOSED (sub-issues #21-26 open for bookkeeping) |
| Provider Expansion Groundwork | [#27](https://github.com/nagicantsleep/k-drive/issues/27) | CLOSED |
| Google Drive Vertical Slice | [#32](https://github.com/nagicantsleep/k-drive/issues/32) | CLOSED |
| Explicit Sync Foundation | [#37](https://github.com/nagicantsleep/k-drive/issues/37) | CLOSED |
| Windows Drive Experience Hardening | [#38](https://github.com/nagicantsleep/k-drive/issues/38) | OPEN |
| OneDrive Vertical Slice | [#39](https://github.com/nagicantsleep/k-drive/issues/39) | OPEN |
| macOS Platform Support | [#40](https://github.com/nagicantsleep/k-drive/issues/40) | OPEN |

## GitHub Commands for Epic Management

```bash
# List all epics
gh issue list --label "epic:*" --state all

# List open epic issues
gh issue list --label "epic:*" --state open

# List issues under a specific epic
gh issue list --label "epic:google-drive-vertical-slice"

# View epic details
gh issue view <issue-number>
```

---

## Historical Reference (Pre-GitHub Migration)

The content below is preserved from the original roadmap document dated March 11, 2026.

### Completed Epics

1. **epic:mvp-foundation** - Established desktop app skeleton, backend module layout, and baseline architecture.
2. **epic:s3-real-mount-foundation** - Replaced placeholder mount behavior with real local persistence, config, and process orchestration.
3. **epic:s3-vertical-slice** - Delivered first end-to-end provider flow for the product.
4. **epic:s3-operational-hardening** - Made Windows S3 mount experience operationally usable.
5. **epic:provider-expansion-groundwork** - Added provider-neutral foundations for Google Drive and OneDrive.

### Planned Epics

6. **epic:google-drive-vertical-slice** - Google Drive connector and OAuth-based account onboarding.
7. **epic:explicit-sync-foundation** - Make sync a first-class feature with status visibility and conflict handling.
8. **epic:windows-drive-experience-hardening** - Improve Windows mount UX with drive letter selection and better diagnostics.
9. **epic:onedrive-vertical-slice** - Microsoft OAuth flow and OneDrive connector implementation.
10. **epic:macos-platform-support** - macOS Keychain integration and macFUSE support.
