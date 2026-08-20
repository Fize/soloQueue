import type { NotificationPayload } from '@/types/chat'

// ─── Browser Notification Manager ────────────────────────────────────────────
// Uses the Web Notification API when the browser grants permission.

class NotificationManager {
  private enabled = true
  private permissionChecked = false

  /** Returns whether the Notification API is available. */
  static isSupported(): boolean {
    return typeof window !== 'undefined' && 'Notification' in window
  }

  /**
   * Ensure permission is granted. On first call, triggers the browser
   * permission dialog. Subsequent calls are silent.
   */
  private async ensurePermission(): Promise<boolean> {
    if (!NotificationManager.isSupported()) return false

    if (Notification.permission === 'granted') return true
    if (Notification.permission === 'denied') return false

    if (!this.permissionChecked) {
      this.permissionChecked = true
      const perm = await Notification.requestPermission()
      return perm === 'granted'
    }
    return false
  }

  /**
   * Show a system notification.
   *
   * @param payload - The notification data from the backend.
   * @param onClick  - Optional callback when the user clicks the notification.
   */
  async show(payload: NotificationPayload, onClick?: () => void): Promise<void> {
    if (!this.enabled) return
    if (!(await this.ensurePermission())) return

    const title = payload.title
    const body = payload.body

    const notif = new Notification(title, {
      body,
      icon: '/icon.png',
      tag: payload.category, // same category = replace previous
    })

    if (onClick) {
      notif.onclick = () => {
        window.focus()
        onClick()
        notif.close()
      }
    }
  }

  /** Enable or disable all notifications globally. */
  setEnabled(v: boolean): void {
    this.enabled = v
  }

  /** Check if notifications are enabled. */
  isEnabled(): boolean {
    return this.enabled
  }
}

export const notificationManager = new NotificationManager()
