import { Badge } from '../atoms'
import type { Instance } from '../../generated/types'

interface Props {
  instance: Instance
  tabCount: number
  selected: boolean
  onClick: () => void
}

export default function InstanceListItem({
  instance,
  tabCount,
  selected,
  onClick,
}: Props) {
  const statusColor =
    instance.status === 'running'
      ? 'bg-success'
      : instance.status === 'error'
        ? 'bg-destructive'
        : 'bg-text-muted'

  const badgeVariant =
    instance.status === 'running'
      ? 'success'
      : instance.status === 'error'
        ? 'danger'
        : 'default'

  return (
    <button
      onClick={onClick}
      className={`mb-1 flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left transition-all ${
        selected
          ? 'bg-primary/10 border border-primary'
          : 'border border-transparent hover:bg-bg-elevated'
      }`}
    >
      <div className={`h-2 w-2 rounded-full ${statusColor}`} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-text-primary">
          {instance.profileName}
        </div>
        <div className="text-xs text-text-muted">
          :{instance.port} · {tabCount} tabs
        </div>
      </div>
      <Badge variant={badgeVariant}>{instance.status}</Badge>
    </button>
  )
}
