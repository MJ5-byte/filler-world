import { useRef, useState } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { api } from '../api'
import type { AppContext } from '../App'

const LANG_HINTS: Record<string, string> = {
  binary: 'A statically linked Linux x86-64 executable that speaks the Filler protocol on stdin/stdout.',
  rust: 'A single main.rs compiled with rustc -O. Std only — no cargo, no crates (builds run offline).',
}
const LANGS = ['rust', 'binary']

export default function Upload() {
  const { user } = useOutletContext<AppContext>()
  const [name, setName] = useState('')
  const [language, setLanguage] = useState('rust')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [over, setOver] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const nav = useNavigate()

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!file) { setError('Choose a file first.'); return }
    setBusy(true)
    setError('')
    const form = new FormData()
    form.set('name', name)
    form.set('language', language)
    form.set('file', file)
    try {
      const res = await api.uploadBot(form)
      nav(`/bots/${res.id}`)
    } catch (err) {
      setError(String(err instanceof Error ? err.message : err))
      setBusy(false)
    }
  }

  return (
    <>
      <h1>Upload a bot</h1>
      <p className="muted">Uploading as <strong>{user?.login}</strong>.</p>
      <form className="upload" onSubmit={submit}>
        <div
          className={'dropzone' + (over ? ' over' : '')}
          onClick={() => fileInput.current?.click()}
          onDragOver={e => { e.preventDefault(); setOver(true) }}
          onDragLeave={() => setOver(false)}
          onDrop={e => {
            e.preventDefault()
            setOver(false)
            if (e.dataTransfer.files[0]) setFile(e.dataTransfer.files[0])
          }}
        >
          <div className="dropzone-title">{file ? file.name : 'DROP BOT FILE HERE'}</div>
          <div className="dropzone-sub">{file ? 'click to choose a different file' : 'or click to browse — single source file or a Linux binary'}</div>
          <input ref={fileInput} type="file" hidden
            onChange={e => setFile(e.target.files?.[0] ?? null)} />
        </div>

        <label style={{ marginTop: 6 }}>Language</label>
        <div className="chip-row" style={{ margin: 0 }}>
          {LANGS.map(l => (
            <span key={l} className={`chip ${language === l ? 'on' : ''}`} onClick={() => setLanguage(l)}>
              {l.toUpperCase()}
            </span>
          ))}
        </div>
        <p className="muted" style={{ margin: 0 }}>{LANG_HINTS[language]}</p>

        <label>Bot name
          <input type="text" value={name} onChange={e => setName(e.target.value)}
            placeholder="my_destroyer" required pattern="[a-zA-Z0-9_\-]{2,40}" />
        </label>

        {error && <div className="error-box">{error}</div>}
        <button disabled={busy}>{busy ? 'Deploying…' : 'Deploy bot'}</button>
        <p className="muted" style={{ margin: 0 }}>
          After upload your bot is built in an offline sandbox, then automatically played
          through a set of required matches for review. Once accepted, challenge other bots
          to start climbing the leaderboard. Build errors appear on the bot page.
        </p>
      </form>
    </>
  )
}
