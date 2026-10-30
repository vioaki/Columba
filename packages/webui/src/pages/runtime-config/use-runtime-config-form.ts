import { useCallback, useEffect, useRef, useState } from 'react'
import type { RuntimeConfig, UsageLogRuntimeConfig, LogRetentionConfig } from '@/lib/types'

/** 目录同步周期的表单默认（与后端 ResolveModelCatalogInterval 默认一致）。 */
const defaultCatalogSyncMinutes = 1440


/** 归一化后的表单类型：数据入口 effect 补齐 usageLog/modelCatalog/outbound/agentRemote 后必非空。 */
export type RuntimeConfigForm = Omit<
  RuntimeConfig,
  'usageLog' | 'modelCatalog' | 'outbound'
> & {
  usageLog: UsageLogRuntimeConfig
  modelCatalog: NonNullable<RuntimeConfig['modelCatalog']>
  outbound: NonNullable<RuntimeConfig['outbound']>
}


/**
 * 运行配置表单：数据入口一次性补默认块并快照 pristine 基线；更新走三个
 * 命名入口（顶层字段 / usageLog 子字段 / 出站禁止段文本）。保存时经
 * dirtyBlockPayload 只回传用户实际改过的块——页面开着期间 agent 工具或
 * 同事手改的配置不会被本页的旧快照悄悄覆盖回去。
 */
export function useRuntimeConfigForm(data: RuntimeConfig | undefined) {
  const [form, setForm] = useState<RuntimeConfigForm | null>(null)
  const pristineRef = useRef<RuntimeConfigForm | null>(null)
  const lastBodyMaxKB = useRef(1024)

  const reset = useCallback((saved: RuntimeConfig) => {
    const normalized: RuntimeConfigForm = {
      ...saved,
      modelCatalog: saved.modelCatalog ?? {
        enabled: true, url: '', syncIntervalMinutes: defaultCatalogSyncMinutes,
      },
      outbound: saved.outbound ?? { deniedIpRanges: [] },
    }
    lastBodyMaxKB.current = saved.usageLog.bodyMaxKB > 0 ? saved.usageLog.bodyMaxKB : 1024
    pristineRef.current = normalized
    setForm(normalized)
  }, [])

  useEffect(() => {
    if (data) reset(data)
  }, [data, reset])

  function update<K extends keyof RuntimeConfig>(key: K, value: RuntimeConfig[K]) {
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev))
  }

  function updateUsageLog<K extends keyof UsageLogRuntimeConfig>(key: K, value: UsageLogRuntimeConfig[K]) {
    setForm((prev) => (prev ? { ...prev, usageLog: { ...prev.usageLog, [key]: value } } : prev))
  }

  function updateSystemLog<K extends keyof LogRetentionConfig>(key: K, value: LogRetentionConfig[K]) {
    setForm((prev) => (prev ? { ...prev, systemLog: { ...prev.systemLog, [key]: value } } : prev))
  }

  function toggleUsageBody(enabled: boolean) {
    if (form && form.usageLog.bodyMaxKB > 0) lastBodyMaxKB.current = form.usageLog.bodyMaxKB
    updateUsageLog('bodyMaxKB', enabled ? lastBodyMaxKB.current : 0)
  }

  /** 禁止段编辑：textarea 一行一段，保留原始输入（保存时后端 trim 清洗）。 */
  function updateOutboundText(text: string) {
    setForm((prev) =>
      prev
        ? { ...prev, outbound: { ...prev.outbound, deniedIpRanges: text === '' ? [] : text.split('\n') } }
        : prev,
    )
  }

  /** 恢复出站禁止段为服务端下发的默认段。 */
  function resetOutboundDefaults() {
    setForm((prev) =>
      prev
        ? { ...prev, outbound: { ...prev.outbound, deniedIpRanges: [...(prev.outbound.defaultDeniedIpRanges ?? [])] } }
        : prev,
    )
  }

  /** 脏块 payload：未改过的块不含在返回值里（调用方与恒发字段展开合并）。 */
  function dirtyBlockPayload() {
    if (!form || !pristineRef.current) return {}
    const pristine = pristineRef.current
    return {
      ...(pristine.logDatabasePath !== form.logDatabasePath ? { logDatabasePath: form.logDatabasePath } : {}),
      ...(pristine.logFormat !== form.logFormat ? { logFormat: form.logFormat } : {}),
      ...(pristine.logPersistLevel !== form.logPersistLevel ? { logPersistLevel: form.logPersistLevel } : {}),
      ...(JSON.stringify(pristine.outbound.deniedIpRanges) !== JSON.stringify(form.outbound.deniedIpRanges)
        || (pristine.outbound.proxyUrl ?? '').trim() !== (form.outbound.proxyUrl ?? '').trim()
        ? {
            outbound: {
              ...(JSON.stringify(pristine.outbound.deniedIpRanges) !== JSON.stringify(form.outbound.deniedIpRanges)
                ? { deniedIpRanges: form.outbound.deniedIpRanges.map((entry) => entry.trim()).filter(Boolean) } : {}),
              ...((pristine.outbound.proxyUrl ?? '').trim() !== (form.outbound.proxyUrl ?? '').trim()
                ? { proxyUrl: (form.outbound.proxyUrl ?? '').trim() } : {}),
            },
          }
        : {}),
      ...(JSON.stringify(pristine.systemLog) !== JSON.stringify(form.systemLog) ? { systemLog: form.systemLog } : {}),
      ...(JSON.stringify(pristine.usageLog) !== JSON.stringify(form.usageLog) ? { usageLog: form.usageLog } : {}),
      ...(pristine.modelCatalog.syncIntervalMinutes !== form.modelCatalog.syncIntervalMinutes
        ? { modelCatalog: { syncIntervalMinutes: form.modelCatalog.syncIntervalMinutes } }
        : {}),

    }
  }

  const pristine = pristineRef.current
  const isDirty = Boolean(form && pristine && (
    (['host', 'port', 'logLevel', 'logPersistLevel', 'logFormat', 'httpTimeout', 'databasePath', 'logDatabasePath', 'enablePprof'] as const)
      .some((key) => form[key] !== pristine[key]) ||
    Object.keys(dirtyBlockPayload()).length > 0
  ))

  return {
    form,
    reset,
    isDirty,
    update,
    updateUsageLog,
    updateSystemLog,
    toggleUsageBody,
    updateOutboundText,
    resetOutboundDefaults,
    dirtyBlockPayload,
  }
}
