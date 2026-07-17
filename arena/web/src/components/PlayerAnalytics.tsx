import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, PlayerStats } from '../api'
import RatingChart from './RatingChart'

const AZURE = 'var(--p1)'
const AMBER = 'var(--p2)'

function Tile({ label, value, sub }: { label: string; value: React.ReactNode; sub?: string }) {
  return (
    <div className="tile">
      <div className="tile-label">{label}</div>
      <div className="tile-value">{value}</div>
      {sub && <div className="tile-sub muted">{sub}</div>}
    </div>
  )
}

export default function PlayerAnalytics({ name }: { name: string }) {
  const [stats, setStats] = useState<PlayerStats | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    api.playerStats(name).then(s => alive && setStats(s)).catch(e => alive && setError(String(e)))
    return () => { alive = false }
  }, [name])

  if (error) return <div className="error-box">{error}</div>
  if (!stats) return <p className="muted">Crunching numbers…</p>
  if (stats.totalMatches === 0) {
    return <p className="muted">No finished matches yet — analytics appear after the first battle.</p>
  }

  const streak = stats.streakCurrent
  const maxDay = Math.max(...stats.activity.map(d => d.count), 1)

  return (
    <div className="analytics">
      <div className="tiles">
        <Tile label="matches" value={stats.totalMatches} sub="vs other players' bots" />
        <Tile
          label="board control"
          value={stats.domination != null ? `${Math.round(stats.domination * 100)}%` : '—'}
          sub="avg share of all cells scored"
        />
        <Tile
          label="current streak"
          value={
            streak === 0 ? '—' : (
              <span className={streak > 0 ? 'result-win' : 'result-loss'}>
                {Math.abs(streak)}{streak > 0 ? 'W' : 'L'}
              </span>
            )
          }
        />
        <Tile label="best win streak" value={<span className="result-win">{stats.streakBest}</span>} />
        <Tile
          label="nemesis"
          value={stats.nemesis
            ? <Link to={`/players/${stats.nemesis.owner}`}>{stats.nemesis.name}</Link>
            : '—'}
          sub={stats.nemesis ? `${stats.nemesis.losses} losses to it` : 'nobody beats you'}
        />
        <Tile
          label="favorite prey"
          value={stats.prey
            ? <Link to={`/players/${stats.prey.owner}`}>{stats.prey.name}</Link>
            : '—'}
          sub={stats.prey ? `beaten ${stats.prey.wins} times` : 'no victims yet'}
        />
      </div>

      <div className="analytics-grid">
        <div className="panel">
          <h3 className="panel-title">Rating history</h3>
          <RatingChart series={stats.ratingHistory} />
        </div>

        <div className="analytics-col">
          <div className="panel">
            <h3 className="panel-title">Per map</h3>
            {stats.perMap.map(m => {
              const total = m.wins + m.losses + m.draws
              return (
                <div className="map-row" key={m.map}
                  title={`${m.map}: ${m.wins} wins, ${m.losses} losses, ${m.draws} draws`}>
                  <span className="map-name">{m.map}</span>
                  <div className="map-bar">
                    {m.wins > 0 && (
                      <div className="map-seg" style={{ flex: m.wins, background: AZURE }} />
                    )}
                    {m.losses > 0 && (
                      <div className="map-seg" style={{ flex: m.losses, background: AMBER }} />
                    )}
                    {m.draws > 0 && (
                      <div className="map-seg" style={{ flex: m.draws, background: 'var(--line)' }} />
                    )}
                  </div>
                  <span className="map-nums">
                    <span className="result-win">{m.wins}</span>–<span className="result-loss">{m.losses}</span>
                    <span className="muted"> / {total}</span>
                  </span>
                </div>
              )
            })}
            <div className="legend" style={{ marginTop: 10 }}>
              <span><span className="swatch" style={{ background: AZURE }} />wins</span>
              <span><span className="swatch" style={{ background: AMBER }} />losses</span>
            </div>
          </div>

          <div className="panel">
            <h3 className="panel-title">Last 14 days</h3>
            <div className="activity">
              {stats.activity.map(d => (
                <div className="activity-col" key={d.day} title={`${d.day}: ${d.count} matches`}>
                  <div
                    className="activity-bar"
                    style={{
                      height: `${d.count === 0 ? 3 : 8 + (d.count / maxDay) * 52}px`,
                      background: d.count === 0 ? 'var(--line-soft)' : AZURE,
                    }}
                  />
                </div>
              ))}
            </div>
            <div className="activity-axis muted">
              <span>{stats.activity[0]?.day.slice(5)}</span>
              <span>today</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
