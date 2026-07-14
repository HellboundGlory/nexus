import * as React from "react"

export function DetailBanner({
  fanartUrl, posterUrl, title, children,
}: {
  fanartUrl: string
  posterUrl: string
  title: string
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
      <div className="absolute inset-0 bg-gradient-to-t from-[var(--color-bg)] via-[var(--color-bg)]/70 to-transparent" />
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
