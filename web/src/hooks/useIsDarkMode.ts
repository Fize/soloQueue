import { useState, useEffect } from 'react'

export function useIsDarkMode() {
  const [isDark, setIsDark] = useState(() =>
    typeof document !== 'undefined' ? !document.documentElement.classList.contains('light') : true
  )

  useEffect(() => {
    if (typeof document === 'undefined') return

    const observer = new MutationObserver(() => {
      setIsDark(!document.documentElement.classList.contains('light'))
    })

    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })

    return () => observer.disconnect()
  }, [])

  return isDark
}
