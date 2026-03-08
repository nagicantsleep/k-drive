import { FormEvent, useEffect, useState } from 'react';
import './App.css';

type Account = {
  id: string;
  provider: string;
  email: string;
  options: Record<string, string>;
};

type MountStatus = {
  accountId: string;
  state: 'stopped' | 'mounting' | 'mounted' | 'failed' | string;
  lastError: string;
};

function App() {
  const [accountId, setAccountId] = useState('');
  const [email, setEmail] = useState('');
  const [bucket, setBucket] = useState('');
  const [region, setRegion] = useState('');
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [statuses, setStatuses] = useState<Record<string, MountStatus>>({});
  const [message, setMessage] = useState('');

  async function refreshAccounts() {
    const nextAccounts = (await (window as any).go.main.App.ListAccounts()) as Account[];
    setAccounts(nextAccounts);

    const nextStatuses: Record<string, MountStatus> = {};
    for (const account of nextAccounts) {
      const status = (await (window as any).go.main.App.AccountMountStatus(account.id)) as MountStatus;
      nextStatuses[account.id] = status;
    }
    setStatuses(nextStatuses);
  }

  useEffect(() => {
    void refreshAccounts();
  }, []);

  async function onCreateAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await (window as any).go.main.App.CreateS3Account({
      accountId,
      email,
      options: {
        bucket,
        region,
      },
    });
    setMessage('S3 account created.');
    setAccountId('');
    setEmail('');
    setBucket('');
    setRegion('');
    await refreshAccounts();
  }

  async function mountAccount(id: string) {
    await (window as any).go.main.App.MountAccount(id);
    const status = (await (window as any).go.main.App.AccountMountStatus(id)) as MountStatus;
    setStatuses((prev) => ({ ...prev, [id]: status }));
  }

  async function unmountAccount(id: string) {
    await (window as any).go.main.App.UnmountAccount(id);
    const status = (await (window as any).go.main.App.AccountMountStatus(id)) as MountStatus;
    setStatuses((prev) => ({ ...prev, [id]: status }));
  }

  return (
    <div className="app">
      <h1>K-Drive S3 Vertical Slice</h1>
      <form className="account-form" onSubmit={onCreateAccount}>
        <input value={accountId} onChange={(e) => setAccountId(e.target.value)} placeholder="Account ID" required />
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" required />
        <input value={bucket} onChange={(e) => setBucket(e.target.value)} placeholder="S3 bucket" required />
        <input value={region} onChange={(e) => setRegion(e.target.value)} placeholder="Region" required />
        <button type="submit">Add S3 Account</button>
      </form>

      {message && <p className="message">{message}</p>}

      <ul className="account-list">
        {accounts.map((account) => (
          <li key={account.id}>
            <div>
              <strong>{account.id}</strong> ({account.provider}) - {account.email}
            </div>
            <div>Bucket: {account.options.bucket} | Region: {account.options.region}</div>
            <div className={`mount-status mount-status--${statuses[account.id]?.state ?? 'stopped'}`}>
              Status: {statuses[account.id]?.state ?? 'stopped'}
              {statuses[account.id]?.state === 'failed' && statuses[account.id]?.lastError && (
                <span className="mount-status__error"> — {statuses[account.id].lastError}</span>
              )}
            </div>
            <div className="actions">
              <button type="button" onClick={() => mountAccount(account.id)}>Mount</button>
              <button type="button" onClick={() => unmountAccount(account.id)}>Unmount</button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

export default App;

