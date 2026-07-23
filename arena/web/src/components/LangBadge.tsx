const LANG_CLASS: Record<string, string> = {
  rust: 'lang-rust',
  binary: 'lang-binary',
  builtin: 'lang-builtin',
}

export default function LangBadge({ lang }: { lang: string }) {
  return <span className={`badge ${LANG_CLASS[lang] ?? ''}`}>{lang}</span>
}
