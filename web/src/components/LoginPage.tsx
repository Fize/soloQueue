import { useState, type FormEvent } from 'react'
import { useAuthStore } from '@/stores/authStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Eye, EyeOff, Loader2, Terminal } from 'lucide-react'

export function LoginPage() {
  const { login, isLoading, error, clearError } = useAuthStore()
  const [user, setUser] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    clearError()
    try {
      await login(user, password)
    } catch {
      // error is set in store
    }
  }

  return (
    <div className="flex min-h-screen">
      {/* ── Left brand panel (desktop only) ── */}
      <div className="hidden lg:flex lg:w-[45%] bg-[#0a2540] items-center justify-center relative overflow-hidden">
        {/* Dot grid pattern */}
        <div
          className="absolute inset-0 opacity-[0.08]"
          style={{
            backgroundImage: 'radial-gradient(circle at 1px 1px, white 1px, transparent 0)',
            backgroundSize: '28px 28px',
          }}
        />

        {/* Glowing orbs */}
        <div className="absolute -top-48 -right-48 w-96 h-96 rounded-full bg-[#635bff]/15 blur-[120px]" />
        <div className="absolute -bottom-48 -left-48 w-96 h-96 rounded-full bg-[#00d4ff]/10 blur-[120px]" />

        {/* Brand content */}
        <div className="relative z-10 text-center px-12 animate-in fade-in slide-in-from-bottom-4 duration-700">
          <div className="inline-flex items-center justify-center w-14 h-14 rounded-xl bg-[#635bff]/20 mb-6 ring-1 ring-[#635bff]/30">
            <Terminal className="w-7 h-7 text-[#635bff]" />
          </div>
          <h1
            className="text-5xl font-bold text-white tracking-tight"
            style={{ fontFamily: 'Geist, sans-serif' }}
          >
            soloqueue
          </h1>
          <p className="mt-4 text-sm leading-relaxed text-[#8898aa] max-w-xs mx-auto">
            Agent workflow orchestration platform
          </p>

          {/* Decorative feature list */}
          <div className="mt-10 space-y-3 text-left max-w-[220px] mx-auto">
            {['Multi-agent orchestration', 'Real-time monitoring', 'Tool execution engine'].map(
              (item, i) => (
                <div
                  key={item}
                  className="flex items-center gap-3 text-sm text-[#6b7a94] animate-in fade-in slide-in-from-bottom-4"
                  style={{ animationDelay: `${400 + i * 150}ms`, animationFillMode: 'backwards' }}
                >
                  <div className="w-1.5 h-1.5 rounded-full bg-[#635bff] shrink-0" />
                  {item}
                </div>
              )
            )}
          </div>
        </div>
      </div>

      {/* ── Right form panel ── */}
      <div className="flex-1 flex items-center justify-center bg-gradient-to-b from-white to-[#f6f9fc] p-6">
        <div className="w-full max-w-sm animate-in fade-in slide-in-from-bottom-4 duration-500">
          {/* Mobile logo only */}
          <div className="lg:hidden text-center mb-10">
            <div className="inline-flex items-center justify-center w-12 h-12 rounded-xl bg-[#635bff]/10 mb-4 ring-1 ring-[#635bff]/20">
              <Terminal className="w-6 h-6 text-[#635bff]" />
            </div>
            <h1
              className="text-3xl font-bold text-[#0a2540] tracking-tight"
              style={{ fontFamily: 'Geist, sans-serif' }}
            >
              soloqueue
            </h1>
            <p className="mt-2 text-sm text-[#8898aa]">Sign in to continue</p>
          </div>

          {/* Desktop heading */}
          <div className="hidden lg:block mb-10">
            <h2 className="text-2xl font-semibold text-[#0a2540] tracking-tight">Sign in</h2>
            <p className="mt-1.5 text-sm text-[#8898aa]">Enter your credentials to continue</p>
          </div>

          <form onSubmit={handleSubmit} className="space-y-5">
            <Input
              id="user"
              label="Username"
              type="text"
              value={user}
              onChange={(e) => setUser(e.target.value)}
              placeholder="Enter your username"
              required
              autoFocus
              autoComplete="username"
              className="h-10"
              error={error && !password ? error : undefined}
            />

            <div className="flex flex-col gap-1.5">
              <label htmlFor="password" className="text-xs font-medium text-muted-foreground">
                Password
              </label>
              <div className="relative">
                <input
                  id="password"
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="flex h-10 w-full rounded-md border bg-transparent px-3 py-1 pr-10 text-sm text-foreground transition-colors outline-none placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
                  placeholder="Enter your password"
                  required
                  autoComplete="current-password"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                  tabIndex={-1}
                  aria-label={showPassword ? 'Hide password' : 'Show password'}
                >
                  {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </div>

            {error && (
              <div className="rounded-lg bg-[#ff2f4c]/8 px-3 py-2.5 text-sm text-[#ff2f4c] ring-1 ring-[#ff2f4c]/15 animate-in fade-in slide-in-from-top-1 duration-200">
                {error}
              </div>
            )}

            <Button type="submit" className="w-full h-10" disabled={isLoading}>
              {isLoading ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Signing in...
                </>
              ) : (
                'Sign in'
              )}
            </Button>
          </form>
        </div>
      </div>
    </div>
  )
}
