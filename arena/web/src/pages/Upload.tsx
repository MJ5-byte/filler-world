import { useRef, useState } from 'react'
import { Link, useNavigate, useOutletContext } from 'react-router-dom'
import { api } from '../api'
import type { AppContext } from '../App'

const LANG_HINTS: Record<string, string> = {
  binary: 'A statically linked Linux x86-64 executable that speaks the Filler protocol on stdin/stdout.',
  python: 'A single Python 3 file. It runs with python3 inside the sandbox — stdlib only.',
  rust: 'A single main.rs compiled with rustc -O. Std only — no cargo, no crates (builds run offline).',
  go: 'A single main.go compiled with CGO_ENABLED=0. Stdlib imports only (builds run offline).',
  c: 'A single main.c compiled with gcc -O2 -static. Stdlib only.',
}

export default function Upload() {
  const { user, authReady } = useOutletContext<AppContext>()
  const [name, setName] = useState('')
  const [language, setLanguage] = useState('python')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [over, setOver] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const nav = useNavigate()

  if (authReady && !user) {
    return (
      <div className="panel center-note">
        <h1>Upload a bot</h1>
        <p className="muted">You need an account so your bot has an owner.</p>
        <Link to="/login"><button>Log in with Reboot01</button></Link>
      </div>
    )
  }

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
        <label>Bot name
          <input type="text" value={name} onChange={e => setName(e.target.value)}
            placeholder="my_destroyer" required pattern="[a-zA-Z0-9_\-]{2,40}" />
        </label>
        <label>Language
          <select value={language} onChange={e => setLanguage(e.target.value)}>
            <option value="python">Python (source)</option>
            <option value="rust">Rust (source)</option>
            <option value="go">Go (source)</option>
            <option value="c">C (source)</option>
            <option value="binary">Precompiled Linux binary</option>
          </select>
        </label>
        <p className="muted" style={{ margin: 0 }}>{LANG_HINTS[language]}</p>

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
          {file ? <strong>{file.name}</strong> : 'Drop your bot file here, or click to browse'}
          <input ref={fileInput} type="file" hidden
            onChange={e => setFile(e.target.files?.[0] ?? null)} />
        </div>

        {error && <div className="error-box">{error}</div>}
        <button disabled={busy}>{busy ? 'Uploading…' : 'Upload & enter the arena'}</button>
        <p className="muted" style={{ margin: 0 }}>
          After upload your bot is built/validated in an offline sandbox, then automatically
          queued against every active bot on all maps. Build errors appear on the bot page.
        </p>
      </form>
    </>
  )
}
