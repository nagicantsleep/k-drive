import { FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import './App.css';

type Account = {
  id: string;
  provider: string;
  email: string;
};

type MountStatus = {
  accountId: string;
  state: 'stopped' | 'mounting' | 'mounted' | 'failed' | string;
  lastError: string;
  errorCategory: string;
};

type SyncStatus = {
  accountId: string;
  state: 'idle' | 'syncing' | 'success' | 'error' | 'conflict' | 'needs_resolve' | 'retrying' | 'offline' | string;
  lastSyncAt: string;
  lastError: string;
  conflictCount: number;
  filesSynced: number;
  bytesTransferred: number;
};

type SyncConflict = {
  id: string;
  accountId: string;
  filePath: string;
  localModTime: string;
  remoteModTime: string;
  resolution: string;
  createdAt: string;
};

type CapabilityField = {
  key: string;
  label: string;
  placeholder: string;
  required: boolean;
  secret: boolean;
};

type ProviderCapability = {
  provider: string;
  label: string;
  authScheme: string;
  fields: CapabilityField[];
};

const CATEGORY_LABELS: Record<string, string> = {
  dependency_missing: 'Dependency missing',
  path_error: 'Path error',
  config_invalid: 'Config invalid',
  process_failed: 'Process failed',
};

const go = (window as any).go.main.App;

function App() {
  const [accountId, setAccountId] = useState('');
  const [provider, setProvider] = useState('');
  const [email, setEmail] = useState('');
  const [oauthClientId, setOauthClientId] = useState('');
  const [providerCapabilities, setProviderCapabilities] = useState<ProviderCapability[]>([]);
  const [formValues, setFormValues] = useState<Record<string, string>>({});

  const [accounts, setAccounts] = useState<Account[]>([]);
  const [statuses, setStatuses] = useState<Record<string, MountStatus>>({});
  const [syncStatuses, setSyncStatuses] = useState<Record<string, SyncStatus>>({});
  const [syncConflicts, setSyncConflicts] = useState<Record<string, SyncConflict[]>>({});
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [formPending, setFormPending] = useState(false);
  const [pendingAction, setPendingAction] = useState<Record<string, boolean>>({});

  const statusesRef = useRef(statuses);
  statusesRef.current = statuses;

  const syncStatusesRef = useRef(syncStatuses);
  syncStatusesRef.current = syncStatuses;

  const accountsRef = useRef(accounts);
  accountsRef.current = accounts;

  const selectedCapability = useMemo(
    () => providerCapabilities.find((capability) => capability.provider === provider) ?? null,
    [provider, providerCapabilities],
  );

  async function refreshStatuses(accountList: Account[]) {
    const next: Record<string, MountStatus> = {};
    for (const account of accountList) {
      try {
        const status = (await go.AccountMountStatus(account.id)) as MountStatus;
        next[account.id] = status;
      } catch {
        next[account.id] = { accountId: account.id, state: 'stopped', lastError: '', errorCategory: '' };
      }
    }
    setStatuses(next);
    return next;
  }

  async function refreshSyncStatuses(accountList: Account[]) {
    try {
      const syncStatusList = (await go.ListSyncStatuses()) as SyncStatus[];
      const next: Record<string, SyncStatus> = {};
      for (const status of syncStatusList) {
        next[status.accountId] = status;
      }
      // Set default for accounts without sync status
      for (const account of accountList) {
        if (!next[account.id]) {
          next[account.id] = {
            accountId: account.id,
            state: 'idle',
            lastSyncAt: '',
            lastError: '',
            conflictCount: 0,
            filesSynced: 0,
            bytesTransferred: 0,
          };
        }
      }
      setSyncStatuses(next);
      return next;
    } catch {
      // If sync status is not available, set defaults
      const next: Record<string, SyncStatus> = {};
      for (const account of accountList) {
        next[account.id] = {
          accountId: account.id,
          state: 'idle',
          lastSyncAt: '',
          lastError: '',
          conflictCount: 0,
          filesSynced: 0,
          bytesTransferred: 0,
        };
      }
      setSyncStatuses(next);
      return next;
    }
  }

  async function refreshAccounts() {
    const nextAccounts = (await go.ListAccounts()) as Account[];
    setAccounts(nextAccounts);
    await Promise.all([refreshStatuses(nextAccounts), refreshSyncStatuses(nextAccounts)]);
    return nextAccounts;
  }

  async function refreshCapabilities() {
    const nextCapabilities = (await go.ProviderCapabilities()) as ProviderCapability[];
    setProviderCapabilities(nextCapabilities);
    if (!provider && nextCapabilities.length > 0) {
      setProvider(nextCapabilities[0].provider);
    }
    return nextCapabilities;
  }

  useEffect(() => {
    void Promise.all([refreshCapabilities(), refreshAccounts()]).finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!selectedCapability) {
      setFormValues({});
      return;
    }

    setFormValues((current) => {
      const next: Record<string, string> = {};
      for (const field of selectedCapability.fields) {
        next[field.key] = current[field.key] ?? '';
      }
      return next;
    });
  }, [selectedCapability]);

  // Poll status while any account is in 'mounting' state.
  useEffect(() => {
    const hasMounting = Object.values(statusesRef.current).some((s) => s.state === 'mounting');
    if (!hasMounting) return;

    const interval = setInterval(async () => {
      const updated = await refreshStatuses(accountsRef.current);
      const stillMounting = Object.values(updated).some((s) => s.state === 'mounting');
      if (!stillMounting) clearInterval(interval);
    }, 2000);

    return () => clearInterval(interval);
  }, [statuses]);

  // Poll sync status while any account is syncing.
  useEffect(() => {
    const hasSyncing = Object.values(syncStatusesRef.current).some((s) => s.state === 'syncing' || s.state === 'retrying');
    if (!hasSyncing) return;

    const interval = setInterval(async () => {
      const updated = await refreshSyncStatuses(accountsRef.current);
      const stillSyncing = Object.values(updated).some((s) => s.state === 'syncing' || s.state === 'retrying');
      if (!stillSyncing) clearInterval(interval);
    }, 3000);

    return () => clearInterval(interval);
  }, [syncStatuses]);

  async function onCreateAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedCapability) {
      setError('No provider is available.');
      return;
    }

    setError('');
    setMessage('');
    setFormPending(true);
    try {
      if (selectedCapability.authScheme === 'oauth') {
        await go.BeginOAuth({
          provider,
          accountId,
          clientId: oauthClientId,
        });
        await go.CreateOAuthAccount({
          accountId,
          provider,
          email,
        });
        setMessage(`${selectedCapability.label} account connected.`);
      } else {
        await go.CreateAccount({
          accountId,
          provider,
          email,
          options: formValues,
        });
        setMessage(`${selectedCapability.label} account created.`);
      }
      setAccountId('');
      setEmail('');
      setOauthClientId('');
      setFormValues(Object.fromEntries(selectedCapability.fields.map((field) => [field.key, ''])));
      await refreshAccounts();
    } catch (e: any) {
      setError(String(e));
    } finally {
      setFormPending(false);
    }
  }

  async function mountAccount(id: string) {
    setPendingAction((p) => ({ ...p, [id]: true }));
    setError('');
    try {
      await go.MountAccount(id);
      const status = (await go.AccountMountStatus(id)) as MountStatus;
      setStatuses((prev) => ({ ...prev, [id]: status }));
    } catch (e: any) {
      setError(String(e));
      const status = (await go.AccountMountStatus(id).catch(() => null)) as MountStatus | null;
      if (status) setStatuses((prev) => ({ ...prev, [id]: status }));
    } finally {
      setPendingAction((p) => ({ ...p, [id]: false }));
    }
  }

  async function unmountAccount(id: string) {
    setPendingAction((p) => ({ ...p, [id]: true }));
    setError('');
    try {
      await go.UnmountAccount(id);
      const status = (await go.AccountMountStatus(id)) as MountStatus;
      setStatuses((prev) => ({ ...prev, [id]: status }));
    } catch (e: any) {
      setError(String(e));
    } finally {
      setPendingAction((p) => ({ ...p, [id]: false }));
    }
  }

  function categoryLabel(cat: string) {
    return CATEGORY_LABELS[cat] ?? cat;
  }

  function formatSyncTime(isoTime: string): string {
    if (!isoTime) return '';
    try {
      const date = new Date(isoTime);
      const now = new Date();
      const diffMs = now.getTime() - date.getTime();
      const diffMins = Math.floor(diffMs / 60000);
      const diffHours = Math.floor(diffMs / 3600000);
      const diffDays = Math.floor(diffMs / 86400000);

      if (diffMins < 1) return 'just now';
      if (diffMins < 60) return `${diffMins}m ago`;
      if (diffHours < 24) return `${diffHours}h ago`;
      if (diffDays < 7) return `${diffDays}d ago`;
      return date.toLocaleDateString();
    } catch {
      return isoTime;
    }
  }

  if (loading) {
    return (
      <div className="app">
        <h1>K-Drive</h1>
        <p className="loading">Loading accounts…</p>
      </div>
    );
  }

  return (
    <div className="app">
      <h1>K-Drive</h1>
      <form className="account-form" onSubmit={onCreateAccount}>
        <select value={provider} onChange={(e) => setProvider(e.target.value)} required disabled={formPending || providerCapabilities.length === 0}>
          {providerCapabilities.map((capability) => (
            <option key={capability.provider} value={capability.provider}>
              {capability.label}
            </option>
          ))}
        </select>
        <input value={accountId} onChange={(e) => setAccountId(e.target.value)} placeholder="Account ID (letters, digits, - _)" required disabled={formPending} />
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" required disabled={formPending} />
        {selectedCapability?.authScheme === 'oauth' ? (
          <input value={oauthClientId} onChange={(e) => setOauthClientId(e.target.value)} placeholder="OAuth Client ID" required disabled={formPending} />
        ) : (
          selectedCapability?.fields.map((field) => (
            <label key={field.key} className="account-form__field">
              <span>{field.label}</span>
              <input
                value={formValues[field.key] ?? ''}
                onChange={(e) => setFormValues((current) => ({ ...current, [field.key]: e.target.value }))}
                placeholder={field.placeholder}
                required={field.required}
                type={field.secret ? 'password' : 'text'}
                disabled={formPending}
              />
            </label>
          ))
        )}
        <button type="submit" disabled={formPending || !selectedCapability}>
          {formPending
            ? selectedCapability?.authScheme === 'oauth' ? 'Connecting…' : 'Adding…'
            : selectedCapability?.authScheme === 'oauth'
              ? `Connect ${selectedCapability?.label ?? 'Account'}`
              : `Add ${selectedCapability?.label ?? 'Account'}`}
        </button>
      </form>

      {message && <p className="message">{message}</p>}
      {error && <p className="error">{error}</p>}

      <ul className="account-list">
        {accounts.map((account) => {
          const busy = pendingAction[account.id] ?? false;
          const status = statuses[account.id];
          const syncStatus = syncStatuses[account.id];
          const state = status?.state ?? 'stopped';
          const syncState = syncStatus?.state ?? 'idle';
          const isMounting = state === 'mounting';
          const isMounted = state === 'mounted';
          const isFailed = state === 'failed';
          const isStopped = state === 'stopped';
          const isSyncing = syncState === 'syncing' || syncState === 'retrying';
          const hasConflicts = syncStatus?.conflictCount && syncStatus.conflictCount > 0;

          return (
            <li key={account.id}>
              <div>
                <strong>{account.id}</strong> ({account.provider}) — {account.email}
              </div>
              <div className={`mount-status mount-status--${state}`}>
                {isMounting ? 'Connecting…' : `Status: ${state}`}
                {isFailed && status?.lastError && (
                  <div className="mount-status__error-detail">
                    {status.errorCategory && (
                      <span className="mount-status__error-category">{categoryLabel(status.errorCategory)}: </span>
                    )}
                    {status.lastError}
                  </div>
                )}
              </div>
              {isMounted && (
                <div className={`sync-status sync-status--${syncState}`}>
                  {isSyncing ? (
                    <span className="sync-status__syncing">
                      <span className="sync-status__spinner">⟳</span> Syncing…
                    </span>
                  ) : syncState === 'success' && syncStatus?.lastSyncAt ? (
                    <span className="sync-status__success">
                      ✓ Synced {formatSyncTime(syncStatus.lastSyncAt)}
                    </span>
                  ) : syncState === 'error' ? (
                    <span className="sync-status__error">
                      ⚠ Sync error: {syncStatus?.lastError || 'Unknown error'}
                    </span>
                  ) : syncState === 'conflict' || hasConflicts ? (
                    <span className="sync-status__conflict">
                      ⚠ {syncStatus?.conflictCount || 0} conflict{syncStatus?.conflictCount !== 1 ? 's' : ''}
                    </span>
                  ) : syncState === 'offline' ? (
                    <span className="sync-status__offline">
                      ○ Offline
                    </span>
                  ) : (
                    <span className="sync-status__idle">
                      Sync: idle
                    </span>
                  )}
                </div>
              )}
              <div className="actions">
                <button
                  type="button"
                  onClick={() => mountAccount(account.id)}
                  disabled={busy || isMounting || isMounted}
                  title={isMounted ? 'Already mounted' : isMounting ? 'Connecting…' : 'Mount this drive'}
                >
                  {busy && !isFailed ? '…' : 'Mount'}
                </button>
                <button
                  type="button"
                  onClick={() => unmountAccount(account.id)}
                  disabled={busy || isStopped}
                  title={isStopped ? 'Not mounted' : 'Unmount this drive'}
                >
                  {busy && isMounted ? '…' : 'Unmount'}
                </button>
                {isFailed && (
                  <button
                    type="button"
                    className="actions__retry"
                    onClick={() => mountAccount(account.id)}
                    disabled={busy}
                    title="Retry mount"
                  >
                    {busy ? '…' : 'Retry'}
                  </button>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

export default App;
