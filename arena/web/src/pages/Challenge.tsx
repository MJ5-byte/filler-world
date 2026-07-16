import { useEffect, useMemo, useState } from 'react'
import { Link, useOutletContext, useSearchParams } from 'react-router-dom'
import { api, Bot, GameMap } from '../api'
import type { AppContext } from '../App'
import LangBadge from '../components/LangBadge'

export default function Challenge() {
  const { user, authReady } = useOutletContext<AppContext>()
  const [params, setParams] = useSearchParams()
  const [bots, setBots] = useState<Bot[]>([])
  const [maps, setMaps] = useState<GameMap[]>([])
  const [mapId, setMapId] = useState(0)
  const [error, setError] = useState('')
  const [queued, setQueued] = useState<{ id: number; vs: string }[]>([])
  const [busyId, setBusyId] = useState(0)

  const myBotId = Number(params.get('bot')) || 0

  useEffect(() => {
    let alive = true
    api.leaderboard()
      .then(b => { if (alive) setBots(b) })
      .catch(e => alive && setError(String(e)))
    api.maps().then(m => alive && setMaps(m)).catch(() => {})
    return () => { alive = false }
  }, [])

  const mine = useMemo(
    () => bots.filter(b => user && b.owner === user.login),
    [bots, user],
  )
  const myBot = useMemo(() => mine.find(b => b.id === myBotId), [mine, myBotId])
  const opponents = useMemo(() => bots.filter(b => b.id !== myBotId), [bots, myBotId])

  if (authReady && !user) {
    return (
      <div className="panel center-note">
        <h1>Challenge</h1>
        <p className="muted">Log in to send your bots into battle.</p>
        <Link to="/login"><button>Log in with Reboot01</button></Link>
      </div>
    )
  }

  const fight = async (opp: Bot) => {
    if (!myBot) return
    setBusyId(opp.id)
    setError('')
    try {
      const res = await api.createMatch(myBot.id, opp.id, mapId || undefined)
      setQueued(q => [{ id: res.id, vs: opp.name }, ...q])
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e))
    } finally {
      setBusyId(0)
    }
  }

  return (
    <>
      <h1>Challenge</h1>
      <div className="panel toolbar">
        <label className="inline">Fight as
          <select value={myBotId} onChange={e => setParams(e.target.value === '0' ? {} : { bot: e.target.value })}>
            <option value={0}>Pick your bot…</option>
            {mine.map(b => <option key={b.id} value={b.id}>{b.name} ({b.rating?.toFixed(0)})</option>)}
          </select>
        </label>
        <label className="inline">on
          <select value={mapId} onChange={e => setMapId(Number(e.target.value))}>
            <option value={0}>Random map</option>
            {maps.map(m => <option key={m.id} value={m.id}>{m.name} ({m.width}×{m.height})</option>)}
          </select>
        </label>
        {mine.length === 0 && (
          <span className="muted">
            You have no active bots yet — <Link to="/upload">upload one</Link> first.
          </span>
        )}
        {mine.length > 0 && !myBot && <span className="muted">Pick your fighter, then choose a victim below.</span>}
      </div>

      {error && <div className="error-box" style={{ marginTop: 14 }}>{error}</div>}

      {queued.length > 0 && (
        <div className="panel notice-stack" style={{ marginTop: 14 }}>
          {queued.map(q => (
            <div key={q.id}>
              ⚔️ Match <Link to={`/matches/${q.id}`}>#{q.id}</Link> vs <strong>{q.vs}</strong> queued —
              open the match page to watch the replay when it's done.
            </div>
          ))}
        </div>
      )}

      <h2>Opponents</h2>
      <table>
        <thead>
          <tr>
            <th>Bot</th><th>Owner</th><th>Lang</th>
            <th className="num">Rating</th><th className="num">W–L</th><th></th>
          </tr>
        </thead>
        <tbody>
          {opponents.map(o => (
            <tr key={o.id}>
              <td><Link to={`/bots/${o.id}`}>{o.name}</Link></td>
              <td><Link to={`/players/${o.owner}`} className="muted">{o.owner}</Link></td>
              <td><LangBadge lang={o.language} /></td>
              <td className="num">{o.rating?.toFixed(0) ?? '—'}</td>
              <td className="num">
                <span className="result-win">{o.wins ?? 0}</span>–<span className="result-loss">{o.losses ?? 0}</span>
              </td>
              <td className="right">
                <button className="ghost challenge-btn" disabled={!myBot || busyId === o.id}
                  onClick={() => fight(o)}>
                  {busyId === o.id ? 'Queuing…' : '⚔ Challenge'}
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}
