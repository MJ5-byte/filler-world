import { useCallback, useEffect, useState } from 'react'
import { Link, useOutletContext } from 'react-router-dom'
import { api, DBQueryResult, DBTable } from '../api'
import type { AppContext } from '../App'

function cellDisplay(v: unknown): React.ReactNode {
  if (v === null || v === undefined) return <span className="muted">—</span>
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

function ResultTable({ result }: { result: DBQueryResult }) {
  if (result.rows.length === 0) return <p className="muted">No rows.</p>
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>{result.columns.map(c => <th key={c}>{c}</th>)}</tr>
        </thead>
        <tbody>
          {result.rows.map((row, i) => (
            <tr key={i}>
              {result.columns.map(c => <td key={c}>{cellDisplay(row[c])}</td>)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

const LIMIT = 50

export default function AdminDatabase() {
  const { user, authReady } = useOutletContext<AppContext>()
  const [tables, setTables] = useState<DBTable[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [rows, setRows] = useState<DBQueryResult | null>(null)
  const [offset, setOffset] = useState(0)
  const [error, setError] = useState('')

  const [sql, setSql] = useState('')
  const [queryResult, setQueryResult] = useState<DBQueryResult | null>(null)
  const [queryError, setQueryError] = useState('')
  const [queryBusy, setQueryBusy] = useState(false)

  useEffect(() => {
    if (!user?.isAdmin) return
    api.adminDBTables().then(setTables).catch(e => setError(String(e instanceof Error ? e.message : e)))
  }, [user])

  const loadTable = useCallback((table: string, off: number) => {
    setError('')
    api.adminDBTableRows(table, LIMIT, off)
      .then(r => { setRows(r); setOffset(off) })
      .catch(e => setError(String(e instanceof Error ? e.message : e)))
  }, [])

  const openTable = (table: string) => {
    setSelected(table)
    loadTable(table, 0)
  }

  const runQuery = () => {
    if (!sql.trim()) return
    setQueryBusy(true)
    setQueryError('')
    api.adminDBQuery(sql)
      .then(setQueryResult)
      .catch(e => setQueryError(String(e instanceof Error ? e.message : e)))
      .finally(() => setQueryBusy(false))
  }

  if (!authReady) return <p className="muted">Loading…</p>
  if (!user?.isAdmin) {
    return (
      <div className="panel center-note">
        <h1>Admin</h1>
        <p className="muted">This area is for arena admins only.</p>
      </div>
    )
  }

  return (
    <>
      <h1>Database</h1>
      <p className="muted">
        Read-only — SELECT/WITH queries only. Writes go through the actions on the{' '}
        <Link to="/admin">admin page</Link> (block user, delete bot, etc.).
      </p>
      {error && <div className="error-box">{error}</div>}

      <h2>Tables</h2>
      {tables.length === 0 ? <p className="muted">No tables.</p> : (
        <div className="chip-row">
          {tables.map(t => (
            <span key={t.name} className={`chip ${selected === t.name ? 'on' : ''}`} onClick={() => openTable(t.name)}>
              {t.name} <span className="muted">({t.rows})</span>
            </span>
          ))}
        </div>
      )}

      {selected && rows && (
        <>
          <ResultTable result={rows} />
          <div className="toolbar" style={{ marginTop: 10 }}>
            <button className="ghost small" disabled={offset === 0}
              onClick={() => loadTable(selected, Math.max(0, offset - LIMIT))}>
              Prev
            </button>
            <button className="ghost small" disabled={rows.rows.length < LIMIT}
              onClick={() => loadTable(selected, offset + LIMIT)}>
              Next
            </button>
            <span className="muted small-num">offset {offset}</span>
          </div>
        </>
      )}

      <h2>Run query</h2>
      <div className="panel">
        <textarea
          rows={5}
          style={{ width: '100%' }}
          value={sql}
          onChange={e => setSql(e.target.value)}
          placeholder="select * from bots order by id desc limit 20"
        />
        <div className="toolbar" style={{ marginTop: 10 }}>
          <button disabled={queryBusy} onClick={runQuery}>{queryBusy ? 'Running…' : 'Run'}</button>
          {queryResult?.rowCount != null && <span className="muted small-num">{queryResult.rowCount} rows</span>}
          {queryResult?.truncated && <span className="result-loss small-num">results truncated at 500 rows</span>}
        </div>
        {queryError && <div className="error-box" style={{ marginTop: 10 }}>{queryError}</div>}
        {queryResult && <div style={{ marginTop: 10 }}><ResultTable result={queryResult} /></div>}
      </div>
    </>
  )
}
