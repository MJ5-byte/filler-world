import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, Bot, GameMap, Tournament } from '../api'
import { relTime } from '../components/MatchTable'

const FORMAT_LABEL: Record<string, string> = {
  round_robin: 'round robin',
  single_elim: 'single elim',
}

export default function Tournaments() {
  const nav = useNavigate()
  const [tournaments, setTournaments] = useState<Tournament[] | null>(null)
  const [bots, setBots] = useState<Bot[]>([])
  const [maps, setMaps] = useState<GameMap[]>([])
  const [error, setError] = useState('')

  const [name, setName] = useState('')
  const [format, setFormat] = useState('single_elim')
  const [mapId, setMapId] = useState(0)
  const [allBots, setAllBots] = useState(true)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [createError, setCreateError] = useState('')

  useEffect(() => {
    let alive = true
    const load = () =>
      api.tournaments().then(t => alive && setTournaments(t)).catch(e => alive && setError(String(e)))
    load()
    const t = setInterval(load, 5000)
    api.leaderboard().then(b => alive && setBots(b)).catch(() => {})
    api.maps().then(m => alive && setMaps(m)).catch(() => {})
    return () => { alive = false; clearInterval(t) }
  }, [])

  const maxBots = format === 'round_robin' ? 16 : 32
  const chosen = useMemo(() => (allBots ? bots.length : picked.size), [allBots, bots, picked])

  const toggle = (id: number) => {
    setPicked(p => {
      const next = new Set(p)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const create = async () => {
    setBusy(true)
    setCreateError('')
    try {
      const res = await api.createTournament({
        name,
        format,
        botIds: allBots ? [] : [...picked],
        mapId,
      })
      nav(`/tournaments/${res.id}`)
    } catch (e) {
      setCreateError(String(e instanceof Error ? e.message : e))
    } finally {
      setBusy(false)
    }
  }

  if (error) return <div className="error-box">{error}</div>
  if (!tournaments) return <p className="muted">Loading…</p>

  return (
    <>
      <h1>Tournaments</h1>

      <div className="panel tourney-form">
        <div className="toolbar">
          <input placeholder="Tournament name" value={name} maxLength={60}
            onChange={e => setName(e.target.value)} />
          <label className="inline">Format
            <select value={format} onChange={e => setFormat(e.target.value)}>
              <option value="single_elim">Single elimination</option>
              <option value="round_robin">Round robin</option>
            </select>
          </label>
          <label className="inline">Map
            <select value={mapId} onChange={e => setMapId(Number(e.target.value))}>
              <option value={0}>Rotate all maps</option>
              {maps.map(m => <option key={m.id} value={m.id}>{m.name} ({m.width}×{m.height})</option>)}
            </select>
          </label>
          <label className="inline">
            <input type="checkbox" checked={allBots} onChange={e => setAllBots(e.target.checked)} />
            All active bots ({bots.length})
          </label>
          <button disabled={busy || name.trim().length < 2 || chosen < 2 || chosen > maxBots}
            onClick={create}>
            {busy ? 'Creating…' : '🏆 Start tournament'}
          </button>
        </div>
        {!allBots && (
          <div className="bot-pick">
            {bots.map(b => (
              <label key={b.id} className={`pick-chip ${picked.has(b.id) ? 'on' : ''}`}>
                <input type="checkbox" checked={picked.has(b.id)} onChange={() => toggle(b.id)} />
                {b.name} <span className="muted">({b.rating?.toFixed(0) ?? '—'})</span>
              </label>
            ))}
          </div>
        )}
        {chosen > maxBots && (
          <p className="muted">Max {maxBots} bots for {FORMAT_LABEL[format]} — deselect a few.</p>
        )}
        {createError && <div className="error-box" style={{ marginTop: 10 }}>{createError}</div>}
      </div>

      <h2>All tournaments</h2>
      {tournaments.length === 0 ? (
        <div className="panel center-note muted">No tournaments yet — be the first to throw down the gauntlet.</div>
      ) : (
        <div className="panel" style={{ padding: 0 }}>
          {tournaments.map(t => {
            const pct = t.matchesTotal > 0 ? Math.round((t.matchesDone / t.matchesTotal) * 100) : 0
            return (
              <div className="tourney-row" key={t.id}>
                <div style={{ flex: 1 }}>
                  <div className="tourney-row-name"><Link to={`/tournaments/${t.id}`}>{t.name.toUpperCase()}</Link></div>
                  <div className="tourney-row-meta">
                    {(FORMAT_LABEL[t.format] ?? t.format).toUpperCase()} · {t.participants} BOTS · {t.mapName?.toUpperCase() ?? 'ROTATING MAPS'}
                    {t.winnerName && <> · 👑 {t.winnerName}</>}
                  </div>
                </div>
                <div className="tourney-progress">
                  <div className="tourney-progress-fill" style={{ width: `${pct}%` }} />
                </div>
                <div className="tourney-status">
                  <span className={`badge ${t.status}`}>{t.status}</span>
                </div>
                <div className="muted small-num" style={{ width: 90, textAlign: 'right' }}>
                  {relTime(t.finishedAt ?? t.createdAt)}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}
