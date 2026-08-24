import { useEffect, useState } from 'react'

export type WorkspaceKind = 'phone' | 'pad' | 'desktop'
export type DesignLayout = 'single' | 'split'

export interface WorkspaceCapabilities {
  workspace: WorkspaceKind
  canUseDesignMode: boolean
  designLayout: DesignLayout
}

/**
 * Classifies the effective viewport. Keep this pure so the same product
 * boundary can be tested and used by components without device sniffing.
 */
export function classifyWorkspace(width: number, height: number): WorkspaceCapabilities {
  if (width < 720 || height < 600) {
    return { workspace: 'phone', canUseDesignMode: false, designLayout: 'single' }
  }

  if (width < 1200) {
    return {
      workspace: 'pad',
      canUseDesignMode: true,
      designLayout: width < 1000 ? 'single' : 'split',
    }
  }

  if (height >= 700) {
    return { workspace: 'desktop', canUseDesignMode: true, designLayout: 'split' }
  }

  // A wide but short viewport has no editable design capability. Keep it in
  // the compact/single layout until it satisfies the Desktop height guard.
  return { workspace: 'phone', canUseDesignMode: false, designLayout: 'single' }
}

function readViewportCapabilities(): WorkspaceCapabilities {
  if (typeof window === 'undefined') {
    return classifyWorkspace(0, 0)
  }
  return classifyWorkspace(window.innerWidth, window.innerHeight)
}

export function useWorkspaceCapabilities(): WorkspaceCapabilities {
  const [capabilities, setCapabilities] = useState(readViewportCapabilities)

  useEffect(() => {
    const update = () => setCapabilities(readViewportCapabilities())
    window.addEventListener('resize', update)
    window.addEventListener('orientationchange', update)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('orientationchange', update)
    }
  }, [])

  return capabilities
}
