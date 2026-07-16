import { useEffect, useState } from 'react'
import { api, Match } from '../api'
import MatchTable from '../components/MatchTable'

export default function Matches() {
  const [matches, setMatches] = useState<Match[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    const load = () => api.matches().then(m => alive && setMatches(m)).catch(e => alive && setError(String(e)))
    load()
    const t = setInterval(load, 4000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  if (error) return <div className="error-box">{error}</div>
  if (!matches) return <p className="muted">Loading…</p>
  return (
    <>
      <h1>Recent matches</h1>
      <MatchTable matches={matches} />
    </>
  )
}
