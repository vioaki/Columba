import { useState } from 'react'
import { PageHeader } from '@/components/page-header'
import * as Tabs from '@radix-ui/react-tabs'
import { BillingPanel } from './billing'
import { UsageStatsPage } from './usage-stats'
import { UsageLogsPage } from './usage-logs'
export function UsagePage() {
 const [tab, setTab] = useState('costs')
 return <div className="space-y-6"><PageHeader title="用量费用" /><Tabs.Root value={tab} onValueChange={setTab}><Tabs.List className="mb-5 flex gap-2 border-b" aria-label="用量费用视图"><Tabs.Trigger className="rounded-t px-4 py-3 text-sm data-[state=active]:border-b-2 data-[state=active]:border-foreground focus-visible:outline focus-visible:outline-2" value="costs">费用</Tabs.Trigger><Tabs.Trigger className="rounded-t px-4 py-3 text-sm data-[state=active]:border-b-2 data-[state=active]:border-foreground focus-visible:outline focus-visible:outline-2" value="tokens">Token 统计</Tabs.Trigger><Tabs.Trigger className="rounded-t px-4 py-3 text-sm data-[state=active]:border-b-2 data-[state=active]:border-foreground focus-visible:outline focus-visible:outline-2" value="calls">调用明细</Tabs.Trigger></Tabs.List><Tabs.Content value="costs"><BillingPanel /></Tabs.Content><Tabs.Content value="tokens"><UsageStatsPage /></Tabs.Content><Tabs.Content value="calls"><UsageLogsPage /></Tabs.Content></Tabs.Root></div>
}
