import { Link } from 'react-router-dom'
import { Match } from '../api'

export function relTime(iso: string | null): string {
  if (!iso) return '—'
  const s = (Date.now() - new Date(iso).getTime()) / 1000
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

export default function MatchTable({ matches, perspectiveBotId }: {
  matches: Match[]
  perspectiveBotId?: number
}) {
  if (matches.length === 0) {
    return <div className="panel center-note muted">No matches yet — quiet before the storm.</div>
  }
  return (
    <table>
      <thead>
        <tr>
          <th>Match</th><th>Players</th><th>Map</th><th>Status</th>
          <th className="num">Score</th><th>Result</th><th>When</th>
        </tr>
      </thead>
      <tbody>
        {matches.map(m => {
          let result = <span className="muted">—</span>
          if (m.status === 'finished') {
            if (m.winnerId == null) {
              result = <span className="result-draw">draw</span>
            } else if (perspectiveBotId != null) {
              result = m.winnerId === perspectiveBotId
                ? <span className="result-win">win</span>
                : <span className="result-loss">loss</span>
            } else {
              result = <span className="result-win">{m.winnerId === m.botAId ? m.botAName : m.botBName}</span>
            }
          }
          return (
            <tr key={m.id}>
              <td><Link to={`/matches/${m.id}`}>#{m.id}</Link></td>
              <td>
                <Link to={`/bots/${m.botAId}`}>{m.botAName}</Link>
                <span className="muted"> vs </span>
                <Link to={`/bots/${m.botBId}`}>{m.botBName}</Link>
              </td>
              <td className="muted">{m.mapName}</td>
              <td><span className={`badge ${m.status}`}>{m.status}</span></td>
              <td className="num">{m.scoreA != null ? `${m.scoreA} – ${m.scoreB}` : '—'}</td>
              <td>{result}</td>
              <td className="muted">{relTime(m.finishedAt ?? m.createdAt)}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
