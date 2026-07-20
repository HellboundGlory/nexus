const PAGE_SIZES = [25, 50, 100]

export function Pagination({
  page, pageSize, total, onPageChange, onPageSizeChange,
}: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
  onPageSizeChange: (size: number) => void
}) {
  if (total === 0) return null

  const lastPage = Math.max(1, Math.ceil(total / pageSize))
  const first = (page - 1) * pageSize + 1
  const last = Math.min(page * pageSize, total)

  const btn =
    "rounded border border-[var(--color-border)] px-2 py-1 text-xs hover:border-[var(--color-brand)] disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-[var(--color-border)]"

  return (
    <div className="mt-4 flex items-center justify-between gap-4 text-xs text-[var(--color-muted)]">
      <span>
        Showing {first}–{last} of {total}
      </span>
      <div className="flex items-center gap-2">
        <label className="flex items-center gap-1">
          <span>Per page</span>
          <select
            value={pageSize}
            onChange={(e) => onPageSizeChange(Number(e.target.value))}
            className="rounded border border-[var(--color-border)] bg-[var(--color-panel)] px-1 py-1 text-xs"
          >
            {PAGE_SIZES.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>
        </label>
        <button className={btn} disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
          Previous
        </button>
        <span className="tabular-nums">
          {page} / {lastPage}
        </span>
        <button className={btn} disabled={page >= lastPage} onClick={() => onPageChange(page + 1)}>
          Next
        </button>
      </div>
    </div>
  )
}
