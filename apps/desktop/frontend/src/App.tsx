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
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [formPending, setFormPending] = useState(false);
  const [pendingAction, setPendingAction] = useState<Record<string, boolean>>({});

  const statusesRef = useRef(statuses);
  statusesRef.current = statuses;

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

  async function refreshAccounts() {
    const nextAccounts = (await go.ListAccounts()) as Account[];
    setAccounts(nextAccounts);
    await refreshStatuses(nextAccounts);
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
          const state = status?.state ?? 'stopped';
          const isMounting = state === 'mounting';
          const isMounted = state === 'mounted';
          const isFailed = state === 'failed';
          const isStopped = state === 'stopped';

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
