import { join } from 'node:path'

/** Swift sources included in the native app. */
export function nativeAppSources(directory) {
  return [
    'MacSupport.swift', 'PanelBridge.swift',
    'BackendController.swift', 'PanelProcessController.swift',
    'PanelDownloadController.swift', 'main.swift',
  ].map((name) => join(directory, name))
}
