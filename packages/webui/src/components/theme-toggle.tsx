import { Moon, Sun } from 'lucide-react'
import { useTheme } from '@/lib/theme'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
export function ThemeToggle({ tooltip = false }: { tooltip?: boolean } = {}) {
 const { theme, toggleTheme } = useTheme()
 const dark = theme === 'dark'
 const label = dark ? '切换到浅色模式' : '切换到深色模式'
 const Icon = dark ? Sun : Moon
 const button = <button type="button" onClick={toggleTheme} aria-label={label} aria-pressed={dark} title={label} className="inline-flex h-11 w-11 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"><Icon aria-hidden className="h-5 w-5" /></button>
 return tooltip ? <Tooltip><TooltipTrigger asChild>{button}</TooltipTrigger><TooltipContent>{label}</TooltipContent></Tooltip> : button
}
