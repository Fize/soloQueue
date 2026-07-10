import { useUIStore, LanguageMode } from '@/stores/uiStore'
import { ThemeMode } from '@/lib/theme'
import { Select } from '@/components/ui/select'
import { useTranslation } from '@/lib/i18n'
import { Globe, Palette } from 'lucide-react'

export function GeneralTab() {
  const { theme, setTheme, language, setLanguage } = useUIStore()
  const { t } = useTranslation()

  const themeOptions = [
    { value: 'light', label: t('general.themeLight') },
    { value: 'dark', label: t('general.themeDark') },
    { value: 'system', label: t('general.themeSystem') },
  ]

  const languageOptions = [
    { value: 'en', label: 'English' },
    { value: 'zh', label: '简体中文 (Chinese)' },
  ]

  return (
    <div className="space-y-6">
      {/* Language Section */}
      <div className="border rounded-lg bg-card p-5 shadow-sm space-y-4">
        <div className="flex items-start gap-3">
          <Globe className="h-5 w-5 text-muted-foreground mt-0.5" />
          <div className="space-y-1">
            <h3 className="text-sm font-bold text-foreground">{t('general.language')}</h3>
            <p className="text-xs text-muted-foreground">{t('general.languageDesc')}</p>
          </div>
        </div>
        <div className="max-w-xs">
          <Select
            options={languageOptions}
            value={language}
            onChange={(v) => setLanguage(v as LanguageMode)}
          />
        </div>
      </div>

      {/* Appearance & Theme Section */}
      <div className="border rounded-lg bg-card p-5 shadow-sm space-y-4">
        <div className="flex items-start gap-3">
          <Palette className="h-5 w-5 text-muted-foreground mt-0.5" />
          <div className="space-y-1">
            <h3 className="text-sm font-bold text-foreground">{t('general.appearance')}</h3>
            <p className="text-xs text-muted-foreground">{t('general.appearanceDesc')}</p>
          </div>
        </div>
        <div className="max-w-xs">
          <Select
            options={themeOptions}
            value={theme}
            onChange={(v) => setTheme(v as ThemeMode)}
          />
        </div>
      </div>
    </div>
  )
}

export default GeneralTab
