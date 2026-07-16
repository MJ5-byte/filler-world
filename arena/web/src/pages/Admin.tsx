import { useCallback, useEffect, useState } from 'react'
import { Link, useOutletContext } from 'react-router-dom'
import { AdminOverview, api, Bot, Match } from '../api'
import type { AppContext } from '../App'
import LangBadge from '../components/LangBadge'

function Tile({ label, value, warn }: { label: string; value: React.ReactNode; warn?: boolean }) {
  return (
    <div className="tile">
      <div className="tile-label">{label}</div>
      <div className={'tile-value' + (warn ? ' result-loss' : '')}>{value}</div>
    </div>
  )
}

export default function Admin() {
  const { user, authReady } = useOutletContext<AppContext>()
  const [ov, setOv] = useState<AdminOverview | null>(null)
  const [errored, setErrored] = useState<Match[]>([])
  const [bots, setBots] = useState<Bot[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const load = useCallback(() => {
    api.adminOverview().then(setOv).catch(e => setError(String(e)))
    api.matches().then(ms => setErrored(ms.filter(m => m.status === 'error'))).catch(() => {})
    api.bots().then(setBots).catch(() => {})
  }, [])

  useEffect(() => {
    if (!user?.isAdmin) return
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [user, load])

  if (!authReady) return <p className="muted">Loading…</p>
  if (!user?.isAdmin) {
    return (
      <div className="panel center-note">
        <h1>Admin</h1>
        <p className="muted">This area is for arena admins only.</p>
      </div>
    )
  }

  const act = (p: Promise<unknown>, ok: string) => {
    setNotice('')
    p.then(() => { setNotice(ok); load() })
      .catch(e => setNotice(String(e instanceof Error ? e.message : e)))
  }

  return (
    <>
      <h1>Admin</h1>
      {error && <div className="error-box">{error}</div>}
      {notice && <div className="panel" style={{ marginBottom: 14 }}>{notice}</div>}

      {ov && (
        <div className="tiles" style={{ marginBottom: 18 }}>
          <Tile label="build queue" value={ov.queueBuilds} warn={ov.queueBuilds > 5} />
          <Tile label="match queue" value={ov.queueMatches} warn={ov.queueMatches > 50} />
          <Tile label="running" value={ov.matches['running'] ?? 0} />
          <Tile label="errored" value={ov.matches['error'] ?? 0} warn={(ov.matches['error'] ?? 0) > 0} />
          <Tile label="finished 24h" value={ov.finished24h} />
          <Tile label="avg match"
            value={ov.avgDurationSec != null ? `${ov.avgDurationSec.toFixed(1)}s` : '—'} />
          <Tile label="players" value={ov.players} />
        </div>
      )}

      <h2>Errored matches</h2>
      {errored.length === 0 ? <p className="muted">None. Clean sheet.</p> : (
        <table>
          <thead>
            <tr><th>Match</th><th>Players</th><th>Map</th><th>Error</th><th></th></tr>
          </thead>
          <tbody>
            {errored.map(m => (
              <tr key={m.id}>
                <td><Link to={`/matches/${m.id}`}>#{m.id}</Link></td>
                <td>{m.botAName} <span className="muted">vs</span> {m.botBName}</td>
                <td className="muted">{m.mapName}</td>
                <td className="muted" style={{ maxWidth: 380, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {m.error}
                </td>
                <td className="right">
                  <button className="ghost small"
                    onClick={() => act(api.adminRequeue(m.id), `Match #${m.id} requeued.`)}>
                    Requeue
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>Bots</h2>
      <table>
        <thead>
          <tr><th>Bot</th><th>Owner</th><th>Lang</th><th>Status</th><th className="num">Rating</th><th></th></tr>
        </thead>
        <tbody>
          {bots.map(b => (
            <tr key={b.id}>
              <td><Link to={`/bots/${b.id}`}>{b.name}</Link></td>
              <td className="muted">{b.owner}</td>
              <td><LangBadge lang={b.language} /></td>
              <td><span className={`badge ${b.status}`}>{b.status}</span></td>
              <td className="num">{b.rating?.toFixed(0) ?? '—'}</td>
              <td className="right admin-actions">
                {b.language !== 'builtin' && (
                  <>
                    {(b.status === 'active' || b.status === 'inactive') && (
                      <button className="ghost small"
                        onClick={() => act(
                          api.adminSetBotStatus(b.id, b.status === 'active' ? 'inactive' : 'active'),
                          `${b.name} ${b.status === 'active' ? 'deactivated' : 'activated'}.`)}>
                        {b.status === 'active' ? 'Deactivate' : 'Activate'}
                      </button>
                    )}
                    <button className="ghost small danger"
                      onClick={() => {
                        if (confirm(`Delete ${b.name} and ALL its matches? This cannot be undone.`)) {
                          act(api.adminDeleteBot(b.id), `${b.name} deleted.`)
                        }
                      }}>
                      Delete
                    </button>
                  </>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}
