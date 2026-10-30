import { cn } from '@/lib/utils'

export function BrandMark({ size = 'nav', className }: { size?: 'nav' | 'login'; className?: string }) {
  return <div className={cn('flex items-center gap-3', size === 'login' && 'justify-center', className)}>
    <img src={`${import.meta.env.BASE_URL}Columba.svg`} alt="Columba" width={36} height={36} />
    <b className="text-lg font-semibold tracking-tight">Columba</b>
  </div>
}
