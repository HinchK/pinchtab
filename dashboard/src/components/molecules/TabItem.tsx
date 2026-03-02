import { Card } from '../atoms'
import type { InstanceTab } from '../../generated/types'

interface Props {
  tab: InstanceTab
  compact?: boolean
}

export default function TabItem({ tab, compact }: Props) {
  if (compact) {
    return (
      <div className="border-b border-border-subtle py-2">
        <div className="truncate text-sm text-text-primary">
          {tab.title || 'Untitled'}
        </div>
        <div className="truncate text-xs text-text-muted">{tab.url}</div>
      </div>
    )
  }

  return (
    <Card className="p-2">
      <div className="truncate text-sm text-text-primary">
        {tab.title || 'Untitled'}
      </div>
      <div className="truncate text-xs text-text-muted">{tab.url}</div>
    </Card>
  )
}
