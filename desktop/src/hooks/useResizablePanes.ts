import { useState, useEffect, useRef, useCallback } from 'react'
import { useRuntimeStore } from '@/stores/runtimeStore'

const DESIGN_MIN_RATIO = 0.5
const DESIGN_LEFT_MIN = 320
const DESIGN_DEFAULT_LEFT = 420
const DESIGN_DEFAULT_LEFT_SMALL = 380
const RESIZE_HANDLE_WIDTH = 4
const MIN_AREA_WIDTH = 200

export function useResizablePanes(isDesignMode: boolean, activeSessionId: string | null) {
  const [panelWidth, setPanelWidth] = useState(300)
  const [isResizing, setIsResizing] = useState(false)
  const splitContainerRef = useRef<HTMLDivElement>(null)
  const resizeDragRef = useRef<{ startX: number; startPanelWidth: number } | null>(null)
  const hasManuallyResized = useRef(false)
  const isDesignModeRef = useRef(isDesignMode)

  useEffect(() => {
    isDesignModeRef.current = isDesignMode
  }, [isDesignMode])

  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    resizeDragRef.current = { startX: e.clientX, startPanelWidth: panelWidth }
    setIsResizing(true)
  }, [panelWidth])

  useEffect(() => {
    if (!isResizing) return
    const handleMouseUp = () => {
      resizeDragRef.current = null
      setIsResizing(false)
    }
    const handleMouseMove = (e: MouseEvent) => {
      if (e.buttons === 0) {
        handleMouseUp()
        return
      }
      if (!splitContainerRef.current) return
      const rect = splitContainerRef.current.getBoundingClientRect()
      const drag = resizeDragRef.current
      if (!drag) return

      if (isDesignModeRef.current) {
        hasManuallyResized.current = true
      }

      const newWidth = drag.startPanelWidth - (e.clientX - drag.startX)
      let clamped: number
      if (isDesignModeRef.current) {
        const minRight = Math.floor(rect.width * DESIGN_MIN_RATIO)
        const maxRight = Math.max(minRight, rect.width - DESIGN_LEFT_MIN - RESIZE_HANDLE_WIDTH)
        clamped = Math.max(minRight, Math.min(newWidth, maxRight))
      } else {
        const maxWidth = Math.floor(rect.width * 0.6)
        clamped = Math.max(
          MIN_AREA_WIDTH,
          Math.min(newWidth, rect.width - MIN_AREA_WIDTH, maxWidth),
        )
      }
      setPanelWidth(clamped)
      useRuntimeStore.getState().setInspectorPanelWidth(clamped)
      if (clamped !== newWidth) {
        resizeDragRef.current = { startX: e.clientX, startPanelWidth: clamped }
      }
    }
    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }
  }, [isResizing])

  const [containerWidth, setContainerWidth] = useState(0)
  useEffect(() => {
    const el = splitContainerRef.current
    if (!el) {
      setContainerWidth(0)
      return
    }
    setContainerWidth(el.getBoundingClientRect().width)
    const ro = new ResizeObserver(([entry]) => {
      setContainerWidth(entry.contentRect.width)
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [activeSessionId, isDesignMode])

  useEffect(() => {
    if (!isDesignMode || containerWidth <= 0) {
      return
    }
    const minRight = Math.floor(containerWidth * DESIGN_MIN_RATIO)
    const maxRight = Math.max(minRight, containerWidth - DESIGN_LEFT_MIN - RESIZE_HANDLE_WIDTH)

    if (hasManuallyResized.current) {
      setPanelWidth((current) => {
        const next = Math.max(minRight, Math.min(current, maxRight))
        useRuntimeStore.getState().setInspectorPanelWidth(next)
        return next
      })
      return
    }

    const defaultLeft = containerWidth >= 768 ? DESIGN_DEFAULT_LEFT : DESIGN_DEFAULT_LEFT_SMALL
    const targetWidth = Math.min(
      Math.max(containerWidth - defaultLeft - RESIZE_HANDLE_WIDTH, minRight),
      maxRight,
    )
    setPanelWidth(targetWidth)
    useRuntimeStore.getState().setInspectorPanelWidth(targetWidth)
  }, [isDesignMode, containerWidth])

  return {
    panelWidth,
    isResizing,
    splitContainerRef,
    handleResizeStart,
    containerWidth,
  }
}
