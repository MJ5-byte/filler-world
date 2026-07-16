const LANG_CLASS: Record<string, string> = {
  python: 'lang-python',
  rust: 'lang-rust',
  go: 'lang-go',
  c: 'lang-c',
  binary: 'lang-binary',
  builtin: 'lang-builtin',
}

export default function LangBadge({ lang }: { lang: string }) {
  return <span className={`badge ${LANG_CLASS[lang] ?? ''}`}>{lang}</span>
}
