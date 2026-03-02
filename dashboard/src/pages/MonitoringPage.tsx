import { useEffect, useState, useCallback, useRef } from 'react'
import { useAppStore } from '../stores/useAppStore'
import { EmptyState, Button } from '../components/atoms'
import { TabsChart, InstanceListItem, TabItem } from '../components/molecules'
import type { InstanceTab } from '../generated/types'
import * as api from '../services/api'

const POLL_INTERVAL = 30000 // 30 seconds

export default function MonitoringPage() {
  const {
    instances,
    setInstances,
    setInstancesLoading,
    tabsChartData,
    currentTabs,
    addChartDataPoint,
    setCurrentTabs,
  } = useAppStore()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadInstances = async () => {
    setInstancesLoading(true)
    try {
      const data = await api.fetchInstances()
      setInstances(data)
    } catch (e) {
      console.error('Failed to load instances', e)
    } finally {
      setInstancesLoading(false)
    }
  }

  // Fetch tabs for all running instances
  const fetchAllInstanceTabs = useCallback(async () => {
    const runningInstances = instances.filter((i) => i.status === 'running')
    if (runningInstances.length === 0) return

    try {
      // Use aggregated endpoint that returns proper format
      const allTabs = await api.fetchAllTabs()
      const tabsArray = Array.isArray(allTabs) ? allTabs : []

      const timestamp = Date.now()
      const dataPoint: Record<string, number> = { timestamp }
      const tabsByInstance: Record<string, InstanceTab[]> = {}

      // Group tabs by instance
      for (const inst of runningInstances) {
        const instTabs = tabsArray.filter((t) => t.instanceId === inst.id)
        dataPoint[inst.id] = instTabs.length
        tabsByInstance[inst.id] = instTabs
      }

      addChartDataPoint(dataPoint as any)
      setCurrentTabs(tabsByInstance)
    } catch (e) {
      console.error('Failed to fetch tabs:', e)
    }
  }, [instances, addChartDataPoint, setCurrentTabs])

  // Initial load
  useEffect(() => {
    if (instances.length === 0) {
      loadInstances()
    }
  }, [])

  // Poll tabs
  useEffect(() => {
    fetchAllInstanceTabs()
    pollRef.current = setInterval(fetchAllInstanceTabs, POLL_INTERVAL)
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [fetchAllInstanceTabs])

  // Auto-select first running instance
  useEffect(() => {
    if (!selectedId) {
      const firstRunning = instances.find((i) => i.status === 'running')
      if (firstRunning) setSelectedId(firstRunning.id)
    }
  }, [instances, selectedId])

  const handleStop = async (id: string) => {
    try {
      await api.stopInstance(id)
    } catch (e) {
      console.error('Failed to stop instance', e)
    }
  }

  const selectedInstance = instances.find((i) => i.id === selectedId)
  const selectedTabs = selectedId ? currentTabs[selectedId] || [] : []
  const runningInstances = instances.filter((i) => i.status === 'running')

  if (instances.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center p-4">
        <EmptyState title="Waiting for instances..." icon="📡" />
      </div>
    )
  }

  return (
    <div className="flex flex-1 flex-col gap-4 overflow-auto p-4">
      {/* Chart */}
      <TabsChart
        data={tabsChartData}
        instances={runningInstances.map((i) => ({
          id: i.id,
          profileName: i.profileName,
        }))}
        selectedInstanceId={selectedId}
        onSelectInstance={setSelectedId}
      />

      {/* Bottom section */}
      <div className="flex flex-1 gap-4 overflow-hidden">
        {/* Instance list */}
        <div className="w-64 shrink-0 overflow-auto rounded-lg border border-border-subtle bg-bg-surface">
          <div className="border-b border-border-subtle p-3">
            <h3 className="text-sm font-semibold text-text-secondary">
              Instances ({instances.length})
            </h3>
          </div>
          <div className="p-2">
            {instances.map((inst) => (
              <InstanceListItem
                key={inst.id}
                instance={inst}
                tabCount={currentTabs[inst.id]?.length ?? 0}
                selected={selectedId === inst.id}
                onClick={() => setSelectedId(inst.id)}
              />
            ))}
          </div>
        </div>

        {/* Selected instance details */}
        <div className="flex flex-1 flex-col overflow-hidden rounded-lg border border-border-subtle bg-bg-surface">
          {selectedInstance ? (
            <>
              <div className="flex items-center justify-between border-b border-border-subtle p-3">
                <div>
                  <h3 className="text-sm font-semibold text-text-primary">
                    {selectedInstance.profileName}
                  </h3>
                  <div className="text-xs text-text-muted">
                    Port {selectedInstance.port} ·{' '}
                    {selectedInstance.headless ? 'Headless' : 'Headed'}
                  </div>
                </div>
                {selectedInstance.status === 'running' && (
                  <Button
                    size="sm"
                    variant="danger"
                    onClick={() => handleStop(selectedInstance.id)}
                  >
                    Stop
                  </Button>
                )}
              </div>

              {/* Tabs list */}
              <div className="flex-1 overflow-auto p-3">
                <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">
                  Open Tabs ({selectedTabs.length})
                </h4>
                {selectedTabs.length === 0 ? (
                  <div className="py-8 text-center text-sm text-text-muted">
                    No tabs open
                  </div>
                ) : (
                  <div className="space-y-1">
                    {selectedTabs.map((tab) => (
                      <TabItem key={tab.id} tab={tab} />
                    ))}
                  </div>
                )}
              </div>
            </>
          ) : (
            <div className="flex flex-1 items-center justify-center text-sm text-text-muted">
              Select an instance to view details
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
