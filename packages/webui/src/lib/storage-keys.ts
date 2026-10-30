/** localStorage 键名的统一登记处：全站键都带 columba-webui 前缀，集中在此防拼错与碰撞。 */
export const STORAGE_KEYS = {
  theme: 'columba-webui.theme',
  agentPanelWidth: 'columba-webui.agent-panel-width',
  /** agent 输入的发送键模式：enter | ctrl-enter。 */
  agentSendKey: 'columba-webui.agent-send-key',
  /** 审批/提问卡的 Esc 快捷键：on | off。 */
  agentEscAction: 'columba-webui.agent-esc-action',
} as const
