import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, Match, TournamentDetail as Detail, TournamentParticipant } from '../api'
import MatchTable from '../components/MatchTable'

// Mirrors the server's bracket placement: standard seeding into a
// power-of-two bracket, so seeds 1 and 2 can only meet in the final.
function seedSlots(size: number): number[] {
  let slots = [1]
  while (slots.length < size) {
    const n = slots.length * 2
    const next: number[] = []
    for (const s of slots) next.push(s, n + 1 - s)
    slots = next
  }
  return slots
}

const UNKNOWN = -1
const BYE = 0

interface Cell {
  a: number
  b: number
  match: Match | undefined
}

function buildBracket(parts: TournamentParticipant[], matches: Match[]) {
  let size = 1
  while (size < parts.length) size *= 2
  const seedOf = new Map(parts.map(p => [p.botId, p.seed]))
  const matchAt = new Map(
    matches.filter(m => m.round != null).map(m => [`${m.round}:${m.slot}`, m]),
  )
  let entrants = seedSlots(size).map(seed => (seed <= parts.length ? parts[seed - 1].botId : BYE))

  const rounds: Cell[][] = []
  for (let r = 1; entrants.length > 1; r++) {
    const cells: Cell[] = []
    const next: number[] = []
    for (let s = 0; s < entrants.length / 2; s++) {
      const a = entrants[2 * s]
      const b = entrants[2 * s + 1]
      const match = matchAt.get(`${r}:${s}`)
      cells.push({ a, b, match })
      if (a === UNKNOWN || b === UNKNOWN) next.push(UNKNOWN)
      else if (a === BYE) next.push(b)
      else if (b === BYE) next.push(a)
      else if (match && (match.status === 'finished' || match.status === 'error')) {
        if (match.status === 'finished' && match.winnerId != null) next.push(match.winnerId)
        else next.push((seedOf.get(a) ?? 99) < (seedOf.get(b) ?? 99) ? a : b)
      } else next.push(UNKNOWN)
    }
    rounds.push(cells)
    entrants = next
  }
  return { rounds, champion: entrants[0] }
}

function roundName(index: number, total: number): string {
  const fromEnd = total - index
  if (fromEnd === 1) return 'Final'
  if (fromEnd === 2) return 'Semifinals'
  if (fromEnd === 3) return 'Quarterfinals'
  return `Round ${index + 1}`
}

export default function TournamentDetail() {
  const { id } = useParams()
  const [detail, setDetail] = useState<Detail | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const load = () =>
      api.tournament(id!).then(d => {
        if (!alive) return
        setDetail(d)
        if (d.tournament.status === 'running') timer = window.setTimeout(load, 4000)
      }).catch(e => alive && setError(String(e)))
    load()
    return () => { alive = false; clearTimeout(timer) }
  }, [id])

  const nameOf = useMemo(() => {
    const m = new Map<number, TournamentParticipant>()
    detail?.participants.forEach(p => m.set(p.botId, p))
    return (botId: number) => m.get(botId)
  }, [detail])

  if (error) return <div className="error-box">{error}</div>
  if (!detail) return <p className="muted">Loading…</p>

  const { tournament: t, participants, matches, standings } = detail
  const isBracket = t.format === 'single_elim'

  const cell = (botId: number, match: Match | undefined) => {
    if (botId === BYE) return <div className="bracket-bot bye muted">— bye —</div>
    if (botId === UNKNOWN) return <div className="bracket-bot muted">TBD</div>
    const p = nameOf(botId)
    const score = match?.status === 'finished'
      ? (match.botAId === botId ? match.scoreA : match.scoreB)
      : null
    const won = match?.status === 'finished' && match.winnerId === botId
    return (
      <div className={`bracket-bot ${won ? 'winner' : ''}`}>
        <span className="seed muted">#{p?.seed ?? '?'}</span>
        <Link to={`/bots/${botId}`}>{p?.name ?? `bot ${botId}`}</Link>
        {score != null && <span className="bracket-score">{score}</span>}
      </div>
    )
  }

  return (
    <>
      <div className="page-head">
        <h1>{t.name}</h1>
        <span className="badge">{isBracket ? 'single elim' : 'round robin'}</span>
        <span className={`badge ${t.status}`}>{t.status}</span>
        <span className="muted">{t.mapName ?? 'rotating maps'} · {t.participants} bots ·
          {' '}{t.matchesDone}/{t.matchesTotal} matches</span>
      </div>

      {t.winnerName && (
        <div className="panel champion-banner">
          👑 Champion: <Link to={`/bots/${t.winnerId}`}><strong>{t.winnerName}</strong></Link>
        </div>
      )}
      {t.error && <div className="error-box">{t.error}</div>}

      {isBracket ? (
        <>
          <h2>Bracket</h2>
          <div className="bracket">
            {buildBracket(participants, matches).rounds.map((cells, r, all) => (
              <div className="bracket-round" key={r}>
                <div className="bracket-round-name">{roundName(r, all.length)}</div>
                {cells.map((c, s) => (
                  <div className="bracket-match" key={s}>
                    {cell(c.a, c.match)}
                    {cell(c.b, c.match)}
                    {c.match && (
                      <Link className="bracket-link muted" to={`/matches/${c.match.id}`}>
                        {c.match.status === 'finished' ? 'replay ▸'
                          : c.match.status === 'error' ? 'errored'
                          : c.match.status + '…'}
                      </Link>
                    )}
                  </div>
                ))}
              </div>
            ))}
          </div>
        </>
      ) : (
        standings && (
          <>
            <h2>Standings</h2>
            <table>
              <thead>
                <tr>
                  <th className="rank">#</th><th>Bot</th><th className="num">Played</th>
                  <th className="num">W–L–D</th><th className="num">Points</th>
                  <th className="num">Score diff</th>
                </tr>
              </thead>
              <tbody>
                {standings.map((s, i) => {
                  const p = nameOf(s.botId)
                  return (
                    <tr key={s.botId} className={i === 0 ? 'top-row' : ''}>
                      <td className="rank">{i + 1}</td>
                      <td>
                        <Link to={`/bots/${s.botId}`} className="bot-name">{p?.name ?? s.botId}</Link>
                        <span className="muted"> · {p?.owner}</span>
                      </td>
                      <td className="num">{s.played}</td>
                      <td className="num">
                        <span className="result-win">{s.wins}</span>–
                        <span className="result-loss">{s.losses}</span>–{s.draws}
                      </td>
                      <td className="num">{s.points}</td>
                      <td className="num">{s.scoreDiff > 0 ? `+${s.scoreDiff}` : s.scoreDiff}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </>
        )
      )}

      <h2>Matches</h2>
      <MatchTable matches={matches} />
    </>
  )
}
