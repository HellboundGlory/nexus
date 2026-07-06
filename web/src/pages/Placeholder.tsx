export function Placeholder({ title }: { title: string }) {
  return (
    <div className="p-6">
      <h2 className="text-xl font-semibold">{title}</h2>
      <p className="mt-2 text-[var(--color-muted)]">This page ships in a later slice of the Web UI.</p>
    </div>
  )
}
