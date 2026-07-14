import { MIN_SCALE, MAX_SCALE } from "./useGridScale"

export function ScaleSlider({
  value, onChange,
}: {
  value: number
  onChange: (n: number) => void
}) {
  return (
    <div className="ml-auto flex items-center gap-2 text-[var(--color-muted)]">
      <span aria-hidden className="text-xs">▪</span>
      <input
        type="range"
        aria-label="Card size"
        min={MIN_SCALE}
        max={MAX_SCALE}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="h-1 w-28 cursor-pointer accent-[var(--color-brand)]"
      />
      <span aria-hidden className="text-base">◼</span>
    </div>
  )
}
