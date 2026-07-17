import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, Bot } from '../api'
import LangBadge from '../components/LangBadge'

const LANGS = ['ALL', 'PYTHON', 'GO', 'C', 'RUST', 'BINARY']

function winratePct(b: Bot) {
  const played = b.matchesPlayed ?? 0
  const wins = b.wins ?? 0, losses = b.losses ?? 0, draws = b.draws ?? 0
  return {
    win: played > 0 ? (wins / played) * 100 : 0,
    draw: played > 0 ? (draws / played) * 100 : 0,
    loss: played > 0 ? (losses / played) * 100 : 0,
  }
}

function PodiumCard({ bot, rank }: { bot: Bot; rank: number }) {
  const { win, draw, loss } = winratePct(bot)
  const wins = bot.wins ?? 0, losses = bot.losses ?? 0, draws = bot.draws ?? 0
  return (
    <div className="podium-card">
      <div className="podium-bar">
        <div style={{ width: `${win}%`, background: 'var(--p1)' }} />
        <div style={{ width: `${draw}%`, background: 'var(--dim)' }} />
        <div style={{ width: `${loss}%`, background: 'var(--p2)' }} />
      </div>
      <div className="podium-body">
        <div className="podium-top-row">
          <span className={`podium-rank ${rank === 1 ? 'first' : ''}`}>#{rank}</span>
          <LangBadge lang={bot.language} />
        </div>
        <Link to={`/bots/${bot.id}`} className="podium-name">{bot.name}</Link>
        <Link to={`/players/${bot.owner}`} className="podium-owner muted">{bot.owner}</Link>
        <div className="podium-stats-row">
          <span className="podium-rating">{bot.rating?.toFixed(0) ?? '—'}</span>
          <span className="podium-record muted small-num">{wins}-{losses}-{draws}</span>
        </div>
      </div>
      <div className="podium-bar podium-bar-thick">
        <div style={{ width: `${win}%`, background: 'var(--p1)' }} />
        <div style={{ width: `${draw}%`, background: 'var(--dim)' }} />
        <div style={{ width: `${loss}%`, background: 'var(--p2)' }} />
      </div>
    </div>
  )
}

export default function Leaderboard() {
  const [bots, setBots] = useState<Bot[] | null>(null)
  const [error, setError] = useState('')
  const [lang, setLang] = useState('ALL')

  useEffect(() => {
    let alive = true
    const load = () => api.leaderboard().then(b => alive && setBots(b)).catch(e => alive && setError(String(e)))
    load()
    const t = setInterval(load, 5000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  const filtered = useMemo(() => {
    if (!bots) return []
    if (lang === 'ALL') return bots
    return bots.filter(b => b.language.toUpperCase() === lang)
  }, [bots, lang])

  const podium = filtered.slice(0, 3)
  const rest = filtered.slice(3)

  if (error) return <div className="error-box">{error}</div>
  if (!bots) return <p className="muted">Loading…</p>

  return (
    <>
      <div className="page-head">
        <h1>Leaderboard</h1>
        <span className="muted">{bots.length} BOTS RANKED · Elo, K=32, everyone starts at 1200</span>
      </div>

      {podium.length > 0 && (
        <div className="podium-row">
          {podium.map((b, i) => <PodiumCard key={b.id} bot={b} rank={i + 1} />)}
        </div>
      )}

      <div className="chip-row">
        {LANGS.map(l => (
          <span key={l} className={`chip ${lang === l ? 'on' : ''}`} onClick={() => setLang(l)}>
            {l}
          </span>
        ))}
      </div>

      {rest.length === 0 ? (
        <div className="panel center-note muted">
          {podium.length === 0 ? 'No bots ranked yet.' : 'That\'s everyone — no more bots in this filter.'}
        </div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Rank</th><th>Bot</th><th>Owner</th><th>Lang</th>
              <th className="num">Rating</th><th className="num">W-L-D</th>
              <th>Win rate</th>
            </tr>
          </thead>
          <tbody>
            {rest.map((b, i) => {
              const { win, draw, loss } = winratePct(b)
              const wins = b.wins ?? 0, losses = b.losses ?? 0, draws = b.draws ?? 0
              return (
                <tr key={b.id}>
                  <td className="rank">{i + 4}</td>
                  <td><Link to={`/bots/${b.id}`} className="bot-name">{b.name}</Link></td>
                  <td><Link to={`/players/${b.owner}`} className="muted">{b.owner}</Link></td>
                  <td><LangBadge lang={b.language} /></td>
                  <td className="num rating">{b.rating?.toFixed(0) ?? '—'}</td>
                  <td className="num small-num muted">{wins}-{losses}-{draws}</td>
                  <td className="winrate-cell">
                    <div className="winrate">
                      <div className="winrate-fill" style={{ width: `${win}%` }} />
                      <div className="winrate-draw" style={{ width: `${draw}%` }} />
                      <div className="winrate-loss" style={{ width: `${loss}%` }} />
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </>
  )
}
