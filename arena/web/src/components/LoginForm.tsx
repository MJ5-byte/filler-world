import { useState } from 'react'
import { api } from '../api'

// The AUTH://LOGIN terminal panel. Used standalone inside the landing
// page's login modal — no route or outlet context of its own, so the
// caller supplies refreshUser and what to do once a session exists.
export default function LoginForm({ refreshUser, onSuccess }: {
  refreshUser: () => void
  onSuccess: (login: string) => void
}) {
  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [shake, setShake] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const u = await api.login(identifier, password)
      refreshUser()
      onSuccess(u.login)
    } catch (err) {
      setError(String(err instanceof Error ? err.message : err))
      setShake(true)
      setTimeout(() => setShake(false), 500)
      setBusy(false)
    }
  }

  return (
    <form className={'login-panel' + (shake ? ' shake' : '')} onSubmit={submit}>
      <div className="login-titlebar">
        <span className="login-dot" /><span className="login-dot" /><span className="login-dot" />
        <span className="login-titlebar-label">AUTH://LOGIN</span>
      </div>
      <div className="login-body">
        <div className="login-heading">Welcome back<span className="login-cursor">_</span></div>
        <p className="muted login-tagline">
          Sign in with your Reboot01 account. Your credentials go straight to the
          school's auth server — the arena only keeps your username.
        </p>
        <label className="login-prompt">&gt; USERNAME
          <input type="text" value={identifier} autoFocus required
            onChange={e => setIdentifier(e.target.value)} placeholder="your-login" />
        </label>
        <label className="login-prompt">&gt; PASSWORD
          <input type="password" value={password}
            onChange={e => setPassword(e.target.value)} placeholder="••••••••" />
        </label>
        {error && <div className="error-box">{error}</div>}
        <button disabled={busy} style={{ width: '100%', marginTop: 8 }}>
          {busy ? 'AUTHENTICATING…' : 'AUTHENTICATE'}
        </button>
      </div>
    </form>
  )
}
