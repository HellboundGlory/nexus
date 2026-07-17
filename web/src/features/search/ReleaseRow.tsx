import { rowTone, rejectionSummary, needsConfirm, formatSize, formatAge } from "./resolve"
import type { ScoredRelease } from "./types"

export function ReleaseRow({
  release, onGrab, grabbing,
}: {
  release: ScoredRelease
  onGrab: (r: ScoredRelease) => void
  grabbing: boolean
}) {
  const muted = rowTone(release) === "muted"
  const reasons = rejectionSummary(release)
  return (
    <tr className={`border-t border-[var(--color-border)] text-sm ${muted ? "opacity-60" : ""}`}>
      <td className="max-w-0 px-3 py-2">
        <div className="truncate" title={release.title}>{release.title}</div>
        {reasons ? <div className="mt-0.5 truncate text-xs text-[var(--color-warn)]" title={reasons}>{reasons}</div> : null}
      </td>
      <td className="px-3 py-2 text-xs text-[var(--color-muted)]">{release.indexerId}</td>
      <td className="px-3 py-2 text-xs">{formatSize(release.size)}</td>
      <td className="px-3 py-2 text-xs">{formatAge(release.publishDate)}</td>
      {/* seeders is ABSENT on usenet rows and present on torrents even at 0, so
          the null check is the discriminator — never the numeric value. */}
      <td data-testid="seeders-cell" className="px-3 py-2 text-xs">
        {release.seeders != null ? release.seeders : "—"}
      </td>
      <td className="px-3 py-2 text-xs">{release.quality.name}</td>
      <td className="px-3 py-2 text-xs">
        {needsConfirm(release)
          ? <span className="text-[var(--color-warn)]">Rejected</span>
          : <span className="text-[var(--color-ok)]">OK</span>}
      </td>
      <td className="px-3 py-2 text-right">
        <button
          type="button"
          aria-label={`Grab ${release.title}`}
          disabled={grabbing}
          onClick={() => onGrab(release)}
          className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs disabled:opacity-50"
        >
          Grab
        </button>
      </td>
    </tr>
  )
}
