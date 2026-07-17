import { useEffect, useMemo, useState } from 'react'
import { Link, useOutletContext, useSearchParams } from 'react-router-dom'
import { api, Bot, GameMap } from '../api'
import type { AppContext } from '../App'

export default function Challenge() {
  const { user } = useOutletContext<AppContext>()
  const [params, setParams] = useSearchParams()
  const [bots, setBots] = useState<Bot[]>([])
  const [maps, setMaps] = useState<GameMap[]>([])
  const [mapId, setMapId] = useState(0)
  const [oppId, setOppId] = useState(0)
  const [error, setError] = useState('')
  const [queued, setQueued] = useState<{ id: number; vs: string }[]>([])
  const [busy, setBusy] = useState(false)

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
  const opponent = useMemo(() => opponents.find(o => o.id === oppId), [opponents, oppId])

  const fight = async () => {
    if (!myBot || !opponent) return
    setBusy(true)
    setError('')
    try {
      const res = await api.createMatch(myBot.id, opponent.id, mapId || undefined)
      setQueued(q => [{ id: res.id, vs: opponent.name }, ...q])
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <h1>Challenge</h1>

      {mine.length === 0 && (
        <div className="panel toolbar">
          <span className="muted">
            You have no active bots yet — <Link to="/upload">upload one</Link> first.
          </span>
        </div>
      )}

      <div className="picker-grid">
        <div>
          <div className="picker-label">Your bot</div>
          {mine.map(b => (
            <div
              key={b.id}
              className={`picker-card ${myBotId === b.id ? 'selected p1' : ''}`}
              onClick={() => setParams({ bot: String(b.id) })}
            >
              <div>
                <div className="picker-card-name">{b.name}</div>
                <div className="picker-card-meta">{b.language.toUpperCase()} · rating {b.rating?.toFixed(0) ?? '—'}</div>
              </div>
              {myBotId === b.id && <div className="picker-card-selected p1">SELECTED</div>}
            </div>
          ))}
        </div>
        <div>
          <div className="picker-label">Opponent</div>
          {opponents.map(o => (
            <div
              key={o.id}
              className={`picker-card ${oppId === o.id ? 'selected p2' : ''}`}
              onClick={() => setOppId(o.id)}
            >
              <div>
                <div className="picker-card-name">{o.name}</div>
                <div className="picker-card-meta">{o.language.toUpperCase()} · rating {o.rating?.toFixed(0) ?? '—'}</div>
              </div>
              {oppId === o.id && <div className="picker-card-selected p2">SELECTED</div>}
            </div>
          ))}
        </div>
      </div>

      <h2>Map</h2>
      <div className="map-cards">
        <div className={`map-card ${mapId === 0 ? 'selected' : ''}`} onClick={() => setMapId(0)}>
          <div className="map-card-swatch" />
          <div className="map-card-name">RANDOM</div>
          <div className="map-card-dims">any seeded map</div>
        </div>
        {maps.map(m => (
          <div key={m.id} className={`map-card ${mapId === m.id ? 'selected' : ''}`} onClick={() => setMapId(m.id)}>
            <div className="map-card-swatch" />
            <div className="map-card-name">{m.name.toUpperCase()}</div>
            <div className="map-card-dims">{m.width} × {m.height}</div>
          </div>
        ))}
      </div>

      {error && <div className="error-box" style={{ marginTop: 14 }}>{error}</div>}

      <button style={{ marginTop: 24 }} disabled={!myBot || !opponent || busy} onClick={fight}>
        {busy ? 'Queuing…' : 'Deploy fight'}
      </button>

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
    </>
  )
}
