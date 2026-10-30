import type { RangeKey } from '@/components/usage-filter-bar'
import type { SegOption } from '@/components/ui/seg'

/** 用量页共用的时间窗选项。 */
export const RANGE_OPTIONS: SegOption<RangeKey>[] = [
  { value: 'today', label: '今日' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: 'all', label: '全部' },
]
