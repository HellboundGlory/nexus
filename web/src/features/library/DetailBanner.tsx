import * as React from "react"

export function DetailBanner({
  fanartUrl, posterUrl, title, back, children,
}: {
  fanartUrl: string
  posterUrl: string
  title: string
  back?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="relative isolate -mx-6 -mt-6 mb-6 min-h-[320px] overflow-hidden bg-[var(--color-panel-2)]">
      {fanartUrl ? (
        <img
          data-testid="banner-backdrop"
          src={fanartUrl}
          alt=""
          aria-hidden
          className="absolute inset-0 h-full w-full object-cover"
        />
      ) : null}
      {/* darkening gradient so text stays legible and the banner melts into the page */}
      {fanartUrl ? (
        <div className="absolute inset-0 bg-gradient-to-t from-[var(--color-bg)] via-[var(--color-bg)]/70 to-transparent" />
      ) : null}
      {/* back link floats above the banner (top-left) so the full-bleed banner
          never covers it; a translucent chip keeps it legible over any backdrop */}
      {back ? (
        <div className="absolute left-4 top-4 z-20 rounded-md bg-black/50 px-2 py-1 backdrop-blur-sm">
          {back}
        </div>
      ) : null}
      <div className="relative z-10 flex min-h-[320px] items-end gap-6 p-6">
        {posterUrl ? (
          <div className="aspect-[2/3] w-32 shrink-0 overflow-hidden rounded-lg shadow-lg sm:w-40">
            <img src={posterUrl} alt={title} className="h-full w-full object-cover" />
          </div>
        ) : null}
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </div>
  )
}
