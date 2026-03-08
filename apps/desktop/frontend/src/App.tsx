import { FormEvent, useEffect, useRef, useState } from 'react';
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

const go = (window as any).go.main.App;

function App() {
  const [accountId, setAccountId] = useState('');
  const [email, setEmail] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [region, setRegion] = useState('');
  const [bucket, setBucket] = useState('');
  const [accessKeyId, setAccessKeyId] = useState('');
  const [secretAccessKey, setSecretAccessKey] = useState('');

  const [accounts, setAccounts] = useState<Account[]>([]);
  const [statuses, setStatuses] = useState<Record<string, MountStatus>>({});
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [formPending, setFormPending] = useState(false);
  const [pendingAction, setPendingAction] = useState<Record<string, boolean>>({});

  const statusesRef = useRef(statuses);
  statusesRef.current = statuses;

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

  useEffect(() => {
    void refreshAccounts();
  }, []);

  // Poll status while any account is in 'mounting' state.
  useEffect(() => {
    const hasMounting = Object.values(statusesRef.current).some((s) => s.state === 'mounting');
    if (!hasMounting) return;

    const interval = setInterval(async () => {
      const updated = await refreshStatuses(accounts);
      const stillMounting = Object.values(updated).some((s) => s.state === 'mounting');
      if (!stillMounting) clearInterval(interval);
    }, 2000);

    return () => clearInterval(interval);
  }, [statuses, accounts]);

  async function onCreateAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError('');
    setMessage('');
    setFormPending(true);
    try {
      await go.CreateS3Account({
        accountId,
        email,
        options: {
          endpoint,
          region,
          bucket,
          access_key_id: accessKeyId,
          secret_access_key: secretAccessKey,
        },
      });
      setMessage('S3 account created.');
      setAccountId('');
      setEmail('');
      setEndpoint('');
      setRegion('');
      setBucket('');
      setAccessKeyId('');
      setSecretAccessKey('');
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

  return (
    <div className="app">
      <h1>K-Drive</h1>
      <form className="account-form" onSubmit={onCreateAccount}>
        <input value={accountId} onChange={(e) => setAccountId(e.target.value)} placeholder="Account ID (letters, digits, - _)" required disabled={formPending} />
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" required disabled={formPending} />
        <input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="S3 endpoint URL" required disabled={formPending} />
        <input value={region} onChange={(e) => setRegion(e.target.value)} placeholder="Region" required disabled={formPending} />
        <input value={bucket} onChange={(e) => setBucket(e.target.value)} placeholder="Bucket (optional)" disabled={formPending} />
        <input value={accessKeyId} onChange={(e) => setAccessKeyId(e.target.value)} placeholder="Access Key ID" required disabled={formPending} />
        <input value={secretAccessKey} onChange={(e) => setSecretAccessKey(e.target.value)} placeholder="Secret Access Key" type="password" required disabled={formPending} />
        <button type="submit" disabled={formPending}>{formPending ? 'Adding…' : 'Add S3 Account'}</button>
      </form>

      {message && <p className="message">{message}</p>}
      {error && <p className="error">{error}</p>}

      <ul className="account-list">
        {accounts.map((account) => {
          const busy = pendingAction[account.id] ?? false;
          const status = statuses[account.id];
          return (
            <li key={account.id}>
              <div>
                <strong>{account.id}</strong> ({account.provider}) — {account.email}
              </div>
              <div className={`mount-status mount-status--${status?.state ?? 'stopped'}`}>
                Status: {status?.state ?? 'stopped'}
                {status?.state === 'failed' && status?.lastError && (
                  <span className="mount-status__error">
                    {' — '}
                    {status.errorCategory && status.errorCategory !== 'process_failed'
                      ? `[${status.errorCategory}] `
                      : ''}
                    {status.lastError}
                  </span>
                )}
              </div>
              <div className="actions">
                <button type="button" onClick={() => mountAccount(account.id)} disabled={busy}>
                  {busy ? '…' : 'Mount'}
                </button>
                <button type="button" onClick={() => unmountAccount(account.id)} disabled={busy}>
                  {busy ? '…' : 'Unmount'}
                </button>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

export default App;
