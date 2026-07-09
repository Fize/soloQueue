import { useMemo, useState } from 'react'

export interface ActivityDay {
  date: string
  count: number
  level: 0 | 1 | 2 | 3 | 4
}

interface ActivityHeatmapProps {
  data: ActivityDay[]
  days?: number
  loading?: boolean
}

const MONTHS = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']
const DAYS = ['','Mon','','Wed','','Fri','']

// GitHub green palette (level 0 = empty gray)
const LV_COLORS = [
  '#ebedf0',  // 0: empty
  '#9be9a8',  // 1
  '#40c463',  // 2
  '#30a14e',  // 3
  '#216e39',  // 4
]

function fmtLocal(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`
}

export function ActivityHeatmap({ data, days = 365, loading = false }: ActivityHeatmapProps) {
  const [hovered, setHovered] = useState<{ date: string; count: number; x: number; y: number } | null>(null)

  const { rects, monthLabels, cols, rows } = useMemo(() => {
    const map = new Map<string, ActivityDay>()
    for (const d of data) map.set(d.date, d)

    const today = new Date(); today.setHours(0,0,0,0)
    const end = new Date(today)
    if (end.getDay() !== 0) end.setDate(end.getDate() + (7 - end.getDay()))
    const start = new Date(end)
    start.setDate(start.getDate() - days + 1)
    const sdow = start.getDay()
    start.setDate(start.getDate() - (sdow === 0 ? 6 : sdow - 1))
    const weeks = Math.ceil((end.getTime() - start.getTime()) / 86400000 / 7)

    const rects: { x: number; y: number; day: ActivityDay }[] = []
    const ml: { x: number; label: string }[] = []
    let prevM = -1

    for (let col = 0; col < weeks; col++) {
      for (let row = 0; row < 7; row++) {
        const d = new Date(start)
        d.setDate(start.getDate() + col * 7 + row)
        if (d > today) continue
        const ds = fmtLocal(d)
        const day = map.get(ds) || { date: ds, count: 0, level: 0 } as ActivityDay
        rects.push({ x: col, y: row, day })
        if (row === 0) {
          const m = d.getMonth()
          if (m !== prevM) { ml.push({ x: col, label: MONTHS[m] }); prevM = m }
        }
      }
    }

    return { rects, monthLabels: ml, cols: weeks, rows: 7 }
  }, [data, days])

  const totalTokens = useMemo(() => data.reduce((s,d) => s + d.count, 0), [data])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-primary/30 border-t-primary" />
        <span className="text-xs text-muted-foreground font-mono ml-2">Loading…</span>
      </div>
    )
  }

  const cell = 12, gap = 3, step = cell + gap
  const labelW = 30
  const padR = 16  // right padding so last month label isn't clipped
  const svgW = labelW + cols * step + padR
  const svgH = 18 + rows * step

  return (
    <div className="select-none" style={{ maxWidth: '100%' }}>
      <div className="flex items-center justify-between h-4 mb-2">
        <span className="text-[10px] text-muted-foreground/60 font-mono">
          {data.length > 0
            ? `${totalTokens.toLocaleString()} tokens · ${data.length} active days`
            : 'No activity yet'}
        </span>
        {hovered && (
          <span className="text-[10px] text-foreground font-mono">
            {hovered.count.toLocaleString()} tokens · {hovered.date}
          </span>
        )}
      </div>

      <div className="w-full pb-1">
        <svg
          width={String(svgW)}
          height={String(svgH)}
          viewBox={`0 0 ${svgW} ${svgH}`}
          preserveAspectRatio="xMidYMid meet"
          style={{ display: 'block', width: '100%', height: 'auto' }}
        >
          {/* month labels */}
          {monthLabels.map((m, i) => (
            <text
              key={i}
              x={labelW + m.x * step}
              y={12}
              fontSize={10}
              fontFamily="var(--font-mono, monospace)"
              fill="var(--color-muted-foreground, #71717a)"
              fillOpacity={0.5}
            >
              {m.label}
            </text>
          ))}

          {/* day labels */}
          {DAYS.map((label, i) => (
            <text
              key={i}
              x={0}
              y={20 + i * step + cell / 2}
              fontSize={9}
              fontFamily="var(--font-mono, monospace)"
              fill="var(--color-muted-foreground, #71717a)"
              fillOpacity={0.4}
              textAnchor="start"
              dominantBaseline="middle"
            >
              {label}
            </text>
          ))}

          {/* cells */}
          {rects.map((r, i) => (
            <rect
              key={i}
              x={labelW + r.x * step}
              y={16 + r.y * step}
              width={cell}
              height={cell}
              rx={2}
              fill={LV_COLORS[r.day.level]}
              style={{ cursor: 'pointer', transition: 'fill 0.15s' }}
              onMouseEnter={(e) => {
                const svg = (e.target as SVGRectElement).closest('svg')!
                const pt = svg.createSVGPoint()
                setHovered({ date: r.day.date, count: r.day.count, x: pt.x, y: pt.y })
              }}
              onMouseLeave={() => setHovered(null)}
            >
              <title>{`${r.day.count.toLocaleString()} tokens · ${r.day.date}`}</title>
            </rect>
          ))}
        </svg>

        <div className="flex items-center justify-end gap-1 mt-3">
          <span className="text-[9px] text-muted-foreground/40 font-mono">Less</span>
          {LV_COLORS.map((color, i) => (
            <div key={i} style={{ width: 12, height: 12, borderRadius: 2, backgroundColor: color }} />
          ))}
          <span className="text-[9px] text-muted-foreground/40 font-mono">More</span>
        </div>
      </div>
    </div>
  )
}
