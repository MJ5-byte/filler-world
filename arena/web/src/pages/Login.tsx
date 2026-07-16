import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useOutletContext } from 'react-router-dom'
import { api } from '../api'
import type { AppContext } from '../App'

export default function Login() {
  const { refreshUser } = useOutletContext<AppContext>()
  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [shake, setShake] = useState(false)
  const nav = useNavigate()

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const u = await api.login(identifier, password)
      refreshUser()
      nav(`/players/${u.login}`)
    } catch (err) {
      setError(String(err instanceof Error ? err.message : err))
      setShake(true)
      setTimeout(() => setShake(false), 500)
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className={'panel login-panel' + (shake ? ' shake' : '')} onSubmit={submit}>
        <h1 style={{ marginBottom: 4 }}>Welcome back</h1>
        <p className="muted" style={{ marginTop: 0 }}>
          Sign in with your Reboot01 account. Your credentials go straight to the
          school's auth server — the arena only keeps your username.
        </p>
        <label>Username or email
          <input type="text" value={identifier} autoFocus required
            onChange={e => setIdentifier(e.target.value)} placeholder="your-login" />
        </label>
        <label>Password
          <input type="password" value={password}
            onChange={e => setPassword(e.target.value)} placeholder="••••••••" />
        </label>
        {error && <div className="error-box">{error}</div>}
        <button disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>
      </form>
    </div>
  )
}
