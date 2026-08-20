import { FormEvent, useState } from 'react'
import { LockKeyhole, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useConnectionStore } from '@/stores/connectionStore'

export function AuthGate() {
  const username = useConnectionStore((state) => state.username)
  const authState = useConnectionStore((state) => state.authState)
  const authError = useConnectionStore((state) => state.authError)
  const authenticate = useConnectionStore((state) => state.authenticate)
  const loadConfig = useConnectionStore((state) => state.loadConfig)
  const [user, setUser] = useState(username)
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const retry = async () => {
    setSubmitting(true)
    try {
      await loadConfig()
    } finally {
      setSubmitting(false)
    }
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setSubmitting(true)
    try {
      await authenticate(user, password)
      // Do not retain the password in component state after the request. The
      // store also keeps it memory-only for the current page lifetime.
      setPassword('')
    } finally {
      setSubmitting(false)
    }
  }

  const isConfigError = authState === 'error'

  return (
    <main className="flex h-full min-h-screen w-full items-center justify-center bg-background px-4">
      <section
        className="w-full max-w-sm rounded-2xl border border-border/70 bg-card p-7 shadow-xl"
        aria-labelledby="auth-title"
      >
        <div className="mb-6 flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <LockKeyhole className="h-5 w-5" aria-hidden="true" />
          </div>
          <div>
            <h1 id="auth-title" className="text-lg font-semibold text-foreground">
              Backend authentication
            </h1>
            <p className="text-xs text-muted-foreground">Sign in to continue to SoloQueue.</p>
          </div>
        </div>

        {isConfigError ? (
          <div className="space-y-4" role="alert">
            <p className="text-sm text-destructive">{authError || 'Backend authentication is unavailable.'}</p>
            <Button type="button" className="w-full" onClick={() => void retry()} disabled={submitting}>
              {submitting && <Loader2 className="animate-spin" aria-hidden="true" />}
              Retry
            </Button>
          </div>
        ) : (
          <form className="space-y-4" onSubmit={(event) => void submit(event)}>
            <div className="space-y-1.5">
              <label htmlFor="auth-username" className="text-xs font-medium text-muted-foreground">
                Username
              </label>
              <input
                id="auth-username"
                name="username"
                autoComplete="username"
                value={user}
                onChange={(event) => setUser(event.target.value)}
                className="flex h-10 w-full rounded-md border border-border bg-transparent px-3 text-sm text-foreground outline-none focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50"
                disabled={submitting}
                required
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="auth-password" className="text-xs font-medium text-muted-foreground">
                Password
              </label>
              <input
                id="auth-password"
                name="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                className="flex h-10 w-full rounded-md border border-border bg-transparent px-3 text-sm text-foreground outline-none focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50"
                disabled={submitting}
                required
              />
            </div>
            {authError && <p className="text-sm text-destructive" role="alert">{authError}</p>}
            <Button type="submit" className="w-full" disabled={submitting || !user.trim() || !password}>
              {submitting && <Loader2 className="animate-spin" aria-hidden="true" />}
              Sign in
            </Button>
          </form>
        )}
      </section>
    </main>
  )
}
