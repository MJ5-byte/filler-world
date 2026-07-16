import { useMemo, useRef, useState } from 'react'
import { RatingSeries } from '../api'

// Chart-grade series colors, validated for CVD separation and contrast
// against the app surface (#131926): azure, amber, violet.
export const SERIES = ['#1e94cf', '#c77a26', '#8d70ea']

const W = 640
const H = 220
const PAD = { l: 46, r: 90, t: 14, b: 26 }

interface Hover {
  si: number
  pi: number
  x: number
  y: number
}

export default function RatingChart({ series }: { series: RatingSeries[] }) {
  const [hover, setHover] = useState<Hover | null>(null)
  const svgRef = useRef<SVGSVGElement>(null)

  const { lines, yTicks, yOf, xOf } = useMemo(() => {
    const all = series.flatMap(s => s.points.map(p => p.rating)).concat(1200)
    let lo = Math.min(...all), hi = Math.max(...all)
    const pad = Math.max(20, (hi - lo) * 0.12)
    lo -= pad; hi += pad
    const maxN = Math.max(...series.map(s => s.points.length), 2)
    const xOf = (n: number) => PAD.l + ((n - 1) / (maxN - 1)) * (W - PAD.l - PAD.r)
    const yOf = (r: number) => PAD.t + (1 - (r - lo) / (hi - lo)) * (H - PAD.t - PAD.b)
    const step = (hi - lo) / 3
    const yTicks = [0, 1, 2, 3].map(i => Math.round((lo + i * step) / 10) * 10)
    const lines = series.map(s =>
      s.points.map(p => `${xOf(p.n).toFixed(1)},${yOf(p.rating).toFixed(1)}`).join(' '))
    return { lines, yTicks, yOf, xOf, maxN }
  }, [series])

  if (series.length === 0 || series.every(s => s.points.length < 2)) {
    return <p className="muted">Not enough finished matches for a rating history yet.</p>
  }

  const onMove = (e: React.MouseEvent) => {
    const rect = svgRef.current!.getBoundingClientRect()
    const mx = ((e.clientX - rect.left) / rect.width) * W
    const my = ((e.clientY - rect.top) / rect.height) * H
    let bestD = Infinity
    let bestH: Hover | null = null
    series.forEach((s, si) => {
      s.points.forEach((p, pi) => {
        const dx = xOf(p.n) - mx
        const dy = yOf(p.rating) - my
        const d = dx * dx + dy * dy
        if (d < bestD) {
          bestD = d
          bestH = { si, pi, x: xOf(p.n), y: yOf(p.rating) }
        }
      })
    })
    setHover(bestH)
  }

  const hp = hover ? series[hover.si].points[hover.pi] : null

  return (
    <div className="chart-wrap">
      <svg
        ref={svgRef}
        viewBox={`0 0 ${W} ${H}`}
        className="rating-chart"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
        role="img"
        aria-label="Elo rating history per bot"
      >
        {yTicks.map(t => (
          <g key={t}>
            <line x1={PAD.l} x2={W - PAD.r} y1={yOf(t)} y2={yOf(t)} className="grid-line" />
            <text x={PAD.l - 8} y={yOf(t) + 3.5} className="tick-label" textAnchor="end">{t}</text>
          </g>
        ))}
        <text x={W - PAD.r} y={H - 8} className="tick-label" textAnchor="end">
          matches played →
        </text>

        {hover && (
          <line x1={hover.x} x2={hover.x} y1={PAD.t} y2={H - PAD.b} className="crosshair" />
        )}

        {series.map((s, i) => (
          <g key={s.botId}>
            <polyline points={lines[i]} fill="none" stroke={SERIES[i]} strokeWidth={2}
              strokeLinejoin="round" strokeLinecap="round" />
            {/* direct label at the line's end */}
            <text
              x={xOf(s.points[s.points.length - 1].n) + 7}
              y={yOf(s.points[s.points.length - 1].rating) + 3.5}
              className="series-label"
            >
              {s.botName}
            </text>
          </g>
        ))}

        {hover && (
          <circle cx={hover.x} cy={hover.y} r={4.5}
            fill={SERIES[hover.si]} stroke="var(--surface)" strokeWidth={2} />
        )}
      </svg>

      {hover && hp && (
        <div className="chart-tooltip" style={{
          left: `${(hover.x / W) * 100}%`,
          top: `${(hover.y / H) * 100}%`,
        }}>
          <span className="tt-swatch" style={{ background: SERIES[hover.si] }} />
          {series[hover.si].botName} · match {hp.n} · <strong>{Math.round(hp.rating)}</strong>
        </div>
      )}

      <div className="legend chart-legend">
        {series.map((s, i) => (
          <span key={s.botId}>
            <span className="swatch" style={{ background: SERIES[i] }} />{s.botName}
          </span>
        ))}
      </div>
    </div>
  )
}
