import { useId, useMemo, useState, type KeyboardEvent, type PointerEvent } from 'react'
import { VisuallyHidden } from '@chakra-ui/react'
import dayjs from 'dayjs'
import type { MetricPoint, MetricUnit } from '@/domain/monitoring/types'

/** One line on the chart. `label` names it in the legend and the value readout. */
export interface ChartSeries {
  id: string
  label: string
  points: readonly MetricPoint[]
}

interface TimeSeriesChartProps {
  series: readonly ChartSeries[]
  /** Evaluated range in epoch milliseconds; the x axis always spans it, even where data is missing. */
  start: number
  end: number
  /** Spacing of the backend's points; a jump larger than this is drawn as a gap. */
  stepSeconds: number
  /** `percent` pins the y axis to 0–100; other units scale to the data from zero. */
  unit: MetricUnit
  format: (value: number) => string
  /** Accessible name of the chart, e.g. "CPU usage". */
  label: string
}

// The plot is drawn in this fixed coordinate space and stretched to the container with
// preserveAspectRatio="none"; strokes use vector-effect so stretching never thickens them.
const PLOT_WIDTH = 1000
const PLOT_HEIGHT = 100
// A run of points breaks into a new segment when the next point is this many steps away,
// so a host that stopped reporting shows a gap instead of a misleading straight line.
const GAP_STEPS = 1.5

/** Y-axis bounds and tick values for the series in a given unit. */
function yScale(series: readonly ChartSeries[], unit: MetricUnit): { max: number; ticks: number[] } {
  if (unit === 'percent') return { max: 100, ticks: [0, 25, 50, 75, 100] }
  let peak = 0
  for (const item of series) for (const point of item.points) peak = Math.max(peak, point.value)
  if (peak <= 0) return { max: 1, ticks: [0, 1] }
  // A "nice" step (1, 2, 2.5, or 5 × 10ⁿ) so tick labels read as round numbers.
  const raw = peak / 4
  const magnitude = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 2.5, 5, 10].map((factor) => factor * magnitude).find((candidate) => candidate >= raw) ?? raw
  const max = Math.ceil(peak / step) * step
  const ticks: number[] = []
  for (let value = 0; value <= max + step / 2; value += step) ticks.push(value)
  return { max, ticks }
}

/** Splits points into contiguous runs, breaking where the backend had no value. */
function segments(points: readonly MetricPoint[], stepMs: number): MetricPoint[][] {
  const runs: MetricPoint[][] = []
  let current: MetricPoint[] = []
  for (const point of points) {
    const previous = current[current.length - 1]
    if (previous && point.time - previous.time > stepMs * GAP_STEPS) {
      runs.push(current)
      current = []
    }
    current.push(point)
  }
  if (current.length > 0) runs.push(current)
  return runs
}

/** Time-axis label for a tick: clock time inside a day, date and time across days. */
function formatTick(time: number, spanMs: number): string {
  return dayjs(time).format(spanMs > 26 * 3600_000 ? 'MMM D HH:mm' : 'HH:mm')
}

/** Readout time for the scrubber: always includes the date when the range crosses days. */
function formatReadoutTime(time: number, spanMs: number): string {
  return dayjs(time).format(spanMs > 26 * 3600_000 ? 'MMM D, HH:mm' : 'HH:mm:ss')
}

/**
 * Shared, dependency-free line chart for swallow's usage history (server-metrics.md range
 * series), used by the Server detail Monitoring tab and the Monitoring console through
 * `UsageCharts`.
 *
 * Truthfulness rules it enforces: the x axis spans the requested range even when data is
 * sparse, missing timestamps are drawn as gaps rather than interpolated, and an isolated point
 * is still visible as a dot. Percent metrics share a fixed 0–100 scale so two charts never look
 * alike at different scales.
 *
 * Accessibility: the drawing is decorative (`aria-hidden`); the interactive overlay is a
 * keyboard-operable `slider` scrubber (Arrow keys, Home/End) whose `aria-valuetext` reads the
 * time and every series' value at the cursor, and pointer hover drives the same cursor. Series
 * are told apart by legend text as well as colour.
 */
export function TimeSeriesChart({ series, start, end, stepSeconds, unit, format, label }: TimeSeriesChartProps) {
  const readoutId = useId()
  const [cursor, setCursor] = useState<number | null>(null)
  const stepMs = Math.max(1, stepSeconds) * 1000
  const spanMs = Math.max(stepMs, end - start)
  const slots = Math.floor(spanMs / stepMs) + 1
  const scale = useMemo(() => yScale(series, unit), [series, unit])

  const x = (time: number) => ((time - start) / spanMs) * PLOT_WIDTH
  const y = (value: number) => PLOT_HEIGHT - (Math.min(Math.max(value, 0), scale.max) / scale.max) * PLOT_HEIGHT

  // Per series, the value at each step slot, so the scrubber can read every line at one time.
  const valuesBySlot = useMemo(
    () => series.map((item) => {
      const bySlot = new Map<number, number>()
      for (const point of item.points) bySlot.set(Math.round((point.time - start) / stepMs), point.value)
      return bySlot
    }),
    [series, start, stepMs],
  )

  const paths = series.map((item) => {
    const runs = segments(item.points, stepMs)
    const line = runs
      .map((run) => run.map((point, index) => `${index === 0 ? 'M' : 'L'}${x(point.time).toFixed(2)},${y(point.value).toFixed(2)}`).join('')
        // A one-point run would be invisible as a line; repeating it draws a round-capped dot.
        + (run.length === 1 ? `L${x(run[0].time).toFixed(2)},${y(run[0].value).toFixed(2)}` : ''))
      .join('')
    const area = runs
      .filter((run) => run.length > 1)
      .map((run) => `M${x(run[0].time).toFixed(2)},${PLOT_HEIGHT}`
        + run.map((point) => `L${x(point.time).toFixed(2)},${y(point.value).toFixed(2)}`).join('')
        + `L${x(run[run.length - 1].time).toFixed(2)},${PLOT_HEIGHT}Z`)
      .join('')
    return { id: item.id, line, area }
  })

  const xTicks = [0, 0.25, 0.5, 0.75, 1].map((fraction) => start + fraction * spanMs)
  const cursorTime = cursor === null ? null : start + cursor * stepMs
  const readout = cursor === null
    ? null
    : series.map((item, index) => {
      const value = valuesBySlot[index].get(cursor)
      return { id: item.id, label: item.label, text: value === undefined ? 'No data' : format(value) }
    })
  const readoutText = cursorTime === null || readout === null
    ? `${label}. Use the arrow keys to read values over time.`
    : `${formatReadoutTime(cursorTime, spanMs)}: ${readout.map((entry) => (series.length > 1 ? `${entry.label} ${entry.text}` : entry.text)).join(', ')}`

  const moveCursor = (next: number) => setCursor(Math.min(Math.max(next, 0), slots - 1))
  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const current = cursor ?? slots - 1
    const keys: Record<string, number> = { ArrowLeft: current - 1, ArrowRight: current + 1, Home: 0, End: slots - 1 }
    if (event.key in keys) {
      event.preventDefault()
      moveCursor(keys[event.key])
    }
  }
  const handlePointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    if (bounds.width <= 0) return
    moveCursor(Math.round(((event.clientX - bounds.left) / bounds.width) * (slots - 1)))
  }

  return (
    <div className="sw-ts-chart">
      <div className="sw-ts-chart__y-axis" aria-hidden>
        {/* Invisible in-flow copies give the column the width of its widest label. */}
        {scale.ticks.map((tick) => <span key={`size-${tick}`}>{format(tick)}</span>)}
        {scale.ticks.map((tick) => (
          <span key={tick} style={{ top: `${(1 - tick / scale.max) * 100}%` }}>{format(tick)}</span>
        ))}
      </div>
      <div className="sw-ts-chart__plot">
        <svg viewBox={`0 0 ${PLOT_WIDTH} ${PLOT_HEIGHT}`} preserveAspectRatio="none" aria-hidden focusable="false">
          {scale.ticks.map((tick) => (
            <line key={tick} className="sw-ts-chart__grid" x1="0" x2={PLOT_WIDTH} y1={y(tick)} y2={y(tick)} vectorEffect="non-scaling-stroke" />
          ))}
          {paths.map((path, index) => (
            <g key={path.id} data-series-index={index % 6}>
              {series.length === 1 && path.area && <path className="sw-ts-chart__area" d={path.area} />}
              <path className="sw-ts-chart__line" d={path.line} vectorEffect="non-scaling-stroke" />
            </g>
          ))}
        </svg>
        {cursorTime !== null && (
          <div className="sw-ts-chart__cursor" style={{ left: `${(x(cursorTime) / PLOT_WIDTH) * 100}%` }} aria-hidden />
        )}
        <div
          className="sw-ts-chart__scrubber"
          role="slider"
          tabIndex={0}
          aria-label={label}
          aria-describedby={readoutId}
          aria-valuemin={0}
          aria-valuemax={slots - 1}
          aria-valuenow={cursor ?? slots - 1}
          aria-valuetext={readoutText}
          onKeyDown={handleKeyDown}
          onPointerMove={handlePointerMove}
          onPointerLeave={() => setCursor(null)}
          onBlur={() => setCursor(null)}
        />
        {cursorTime !== null && readout !== null && (
          <div
            className="sw-ts-chart__readout"
            data-align={x(cursorTime) > PLOT_WIDTH / 2 ? 'end' : 'start'}
            style={{ left: `${(x(cursorTime) / PLOT_WIDTH) * 100}%` }}
            aria-hidden
          >
            <strong>{formatReadoutTime(cursorTime, spanMs)}</strong>
            {readout.map((entry, index) => (
              <span key={entry.id} data-series-index={index % 6}>
                {series.length > 1 && <i aria-hidden />}
                {series.length > 1 ? `${entry.label}: ${entry.text}` : entry.text}
              </span>
            ))}
          </div>
        )}
      </div>
      <div className="sw-ts-chart__x-axis" aria-hidden>
        {xTicks.map((tick, index) => (
          <span key={tick} data-edge={index === 0 ? 'start' : index === xTicks.length - 1 ? 'end' : undefined} style={{ left: `${((tick - start) / spanMs) * 100}%` }}>
            {formatTick(tick, spanMs)}
          </span>
        ))}
      </div>
      <VisuallyHidden as="p" id={readoutId}>
        {`${label} from ${formatReadoutTime(start, spanMs)} to ${formatReadoutTime(end, spanMs)}.`}
      </VisuallyHidden>
      {series.length > 1 && (
        <ul className="sw-ts-chart__legend" aria-label={`${label} series`}>
          {series.map((item, index) => (
            <li key={item.id} data-series-index={index % 6}><i aria-hidden />{item.label}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
