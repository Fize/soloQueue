import { Outlet } from 'react-router-dom'

export function SettingsLayout() {
  return (
    <div className="h-full overflow-y-auto px-3 py-3 sm:px-6 sm:py-6">
      <div className="mx-auto max-w-3xl">
        <Outlet />
      </div>
    </div>
  )
}
