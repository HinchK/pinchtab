import { useEffect, useRef, useState } from 'react'

interface Props {
  instancePort: string
  tabId: string
  label: string
  url: string
  quality?: number
  maxWidth?: number
  fps?: number
}

type Status = 'connecting' | 'streaming' | 'error'

export default function ScreencastTile({
  instancePort,
  tabId,
  label,
  url,
  quality = 30,
  maxWidth = 800,
  fps = 1,
}: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const [status, setStatus] = useState<Status>('connecting')
  const [fpsDisplay, setFpsDisplay] = useState('—')
  const [sizeDisplay, setSizeDisplay] = useState('—')

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    // Connect directly to instance's screencast WebSocket
    const wsUrl = `ws://localhost:${instancePort}/screencast?tabId=${encodeURIComponent(tabId)}&quality=${quality}&maxWidth=${maxWidth}&fps=${fps}`

    const socket = new WebSocket(wsUrl)
    socket.binaryType = 'arraybuffer'
    socketRef.current = socket

    let frameCount = 0
    let lastFpsTime = Date.now()

    socket.onopen = () => {
      setStatus('streaming')
    }

    socket.onmessage = (evt) => {
      const blob = new Blob([evt.data], { type: 'image/jpeg' })
      const imgUrl = URL.createObjectURL(blob)
      const img = new Image()
      img.onload = () => {
        canvas.width = img.width
        canvas.height = img.height
        ctx.drawImage(img, 0, 0)
        URL.revokeObjectURL(imgUrl)
      }
      img.src = imgUrl

      frameCount++
      const now = Date.now()
      if (now - lastFpsTime >= 1000) {
        setFpsDisplay(`${frameCount} fps`)
        setSizeDisplay(`${(evt.data.byteLength / 1024).toFixed(0)} KB/frame`)
        frameCount = 0
        lastFpsTime = now
      }
    }

    socket.onerror = () => {
      setStatus('error')
    }

    socket.onclose = () => {
      setStatus('error')
    }

    return () => {
      socket.close()
      socketRef.current = null
    }
  }, [instancePort, tabId, quality, maxWidth, fps])

  const statusColor =
    status === 'streaming'
      ? 'bg-success'
      : status === 'connecting'
        ? 'bg-warning'
        : 'bg-destructive'

  return (
    <div className="overflow-hidden rounded-lg border border-border-subtle bg-bg-elevated">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2">
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs text-text-secondary">{label}</span>
          <div className={`h-2 w-2 rounded-full ${statusColor}`} />
        </div>
        <span className="max-w-[200px] truncate text-xs text-text-muted">{url}</span>
      </div>

      {/* Canvas */}
      <div className="relative aspect-video bg-black">
        <canvas
          ref={canvasRef}
          className="h-full w-full object-contain"
          width={800}
          height={600}
        />
        {status === 'error' && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/80 text-sm text-text-muted">
            Connection lost
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="flex justify-between border-t border-border-subtle px-3 py-1 text-xs text-text-muted">
        <span>{fpsDisplay}</span>
        <span>{sizeDisplay}</span>
      </div>
    </div>
  )
}
