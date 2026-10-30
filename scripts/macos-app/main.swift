// Columba macOS 原生壳:托盘服务管理 + 独立进程承载 WebUI。
// 仅依赖系统框架(Cocoa/WebKit),零第三方依赖,用 swiftc -O 编译。
//
// 行为概要:
// - 托盘拥有内嵌后端；独立窗口进程加载面板，关窗即退出并释放 WebKit
// - 数据全部放在 ~/Library/Application Support/Columba(配置/数据库/日志)
// - 首次运行自动生成配置:空闲端口探测(8765→8799→8800…)
// - 状态栏:模板图标 + 端口号/状态,菜单提供快捷操作

import Cocoa
import Security
import ServiceManagement
import UserNotifications
import WebKit
import os

// MARK: - 常量与路径

#if NATIVE_TESTS
let dataDirPath = ProcessInfo.processInfo.environment["COLUMBA_NATIVE_TEST_DATA"]!
#else
let dataDirPath = NSHomeDirectory() + "/Library/Application Support/Columba"
#endif
let configPath = dataDirPath + "/config.json"
let logPath = dataDirPath + "/columba.log"
let backendPath = Bundle.main.bundlePath + "/Contents/MacOS/columba-server"
let versionPath = Bundle.main.bundlePath + "/Contents/Resources/version.txt"
let initialVersion = "v0.0.0"

// MARK: - 配置模型

struct PanelConfig: Codable {
    struct Outbound: Codable { var proxyUrl: String? }
    var outbound: Outbound?
    var host: String = "127.0.0.1"
    var port: Int = 8765
    var databasePath: String = "columba.sqlite3"
    var logLevel: String = "info"
    var httpTimeout: Int = 120
    var secretKeyPath: String = ".master-key"

    init() {}

    // 与后端语义一致:字段缺失/为空对象时按默认值补齐,而不是解析失败,
    // 避免用户手改过的配置因少写一个键就被当作损坏。
    init(from decoder: Decoder) throws {
        self = PanelConfig()
        let container = try decoder.container(keyedBy: CodingKeys.self)
        host = try container.decodeIfPresent(String.self, forKey: .host) ?? host
        port = try container.decodeIfPresent(Int.self, forKey: .port) ?? port
        databasePath = try container.decodeIfPresent(String.self, forKey: .databasePath) ?? databasePath
        logLevel = try container.decodeIfPresent(String.self, forKey: .logLevel) ?? logLevel
        httpTimeout = try container.decodeIfPresent(Int.self, forKey: .httpTimeout) ?? httpTimeout
        secretKeyPath = try container.decodeIfPresent(String.self, forKey: .secretKeyPath) ?? secretKeyPath
        outbound = try container.decodeIfPresent(Outbound.self, forKey: .outbound)
    }
}


// MARK: - 工具函数


func portIsFree(_ port: Int, host: String = "127.0.0.1") -> Bool {
    guard (1...65535).contains(port) else { return false }
    var hints = addrinfo()
    hints.ai_family = AF_UNSPEC
    hints.ai_socktype = SOCK_STREAM
    hints.ai_flags = AI_PASSIVE
    var addresses: UnsafeMutablePointer<addrinfo>?
    guard getaddrinfo(host, String(port), &hints, &addresses) == 0, let first = addresses else { return false }
    defer { freeaddrinfo(first) }
    var current: UnsafeMutablePointer<addrinfo>? = first
    var bound = false
    while let address = current {
        let info = address.pointee
        let fd = socket(info.ai_family, info.ai_socktype, info.ai_protocol)
        if fd >= 0 {
            // 与 Go net.Listen 一致：允许复用已关闭连接的 TIME_WAIT，仍拒绝活跃监听者。
            var reuse: Int32 = 1
            setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &reuse, socklen_t(MemoryLayout<Int32>.size))
            let result = bind(fd, info.ai_addr, info.ai_addrlen)
            close(fd)
            if result != 0 { return false }
            bound = true
        }
        current = info.ai_next
    }
    return bound
}

/// 只在缺失时创建；写入失败/配置损坏时原样保留文件并交给恢复界面。
func loadOrCreateConfig() -> PanelConfig? {
    let fm = FileManager.default
    do {
        if !fm.fileExists(atPath: configPath) {
            try fm.createDirectory(atPath: dataDirPath, withIntermediateDirectories: true)
            let config = PanelConfig()
            let data = try JSONEncoder().encode(config)
            try data.write(to: URL(fileURLWithPath: configPath), options: .atomic)
            try fm.setAttributes([.posixPermissions: 0o600], ofItemAtPath: configPath)
            return config
        }
        let config = try JSONDecoder().decode(PanelConfig.self, from: Data(contentsOf: URL(fileURLWithPath: configPath)))
        guard (1...65535).contains(config.port), !config.host.isEmpty else { return nil }
        return config
    } catch {
        appLogger.error("Unable to read or create configuration: \(error.localizedDescription, privacy: .public)")
        return nil
    }
}

func readBundledVersion() -> String {
    if let text = try? String(contentsOfFile: versionPath, encoding: .utf8) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty { return trimmed }
    }
    return initialVersion
}

/// "v1.2.3" → [1, 2, 3],用于版本比较;容忍 "4-local" 之类的后缀


/// 由 logo 生成 macOS 风格的单色模板图标(黑色 + alpha,自动适配深浅色菜单栏)
func templateIcon(from path: String, size: CGFloat) -> NSImage? {
    guard let source = NSImage(contentsOfFile: path),
          source.size.width > 0 else { return nil }
    let image = NSImage(size: NSSize(width: size, height: size))
    image.lockFocus()
    let bounds = NSRect(x: 0, y: 0, width: size, height: size)
    let srcSize = source.size
    let scale = min(bounds.width / srcSize.width, bounds.height / srcSize.height)
    let drawWidth = srcSize.width * scale, drawHeight = srcSize.height * scale
    let drawRect = NSRect(x: (bounds.width - drawWidth) / 2, y: (bounds.height - drawHeight) / 2,
                          width: drawWidth, height: drawHeight)
    source.draw(in: drawRect)
    if let context = NSGraphicsContext.current?.cgContext {
        context.setFillColor(NSColor.black.cgColor)
        context.setBlendMode(.sourceAtop)
        context.fill(bounds)
    }
    image.unlockFocus()
    image.isTemplate = true
    return image
}

func javascriptString(_ value: String) -> String {
    let data = try! JSONSerialization.data(withJSONObject: [value])
    let array = String(data: data, encoding: .utf8)!
    return String(array.dropFirst().dropLast())
}

// MARK: - 注入 WebUI 的脚本

/// 主题上报:把页面背景色与深浅色状态回传给壳,壳据此着色标题栏等原生区域,
/// 并在用户切换深浅色时实时跟随。
func themeReporterScript() -> WKUserScript {
    let source = """
    (function() {
      function report() {
        try {
          var style = getComputedStyle(document.body);
          window.webkit.messageHandlers.theme.postMessage({
            dark: document.documentElement.classList.contains('dark'),
            background: style.backgroundColor,
            title: document.title
          });
        } catch (e) {}
      }
      new MutationObserver(report).observe(
        document.documentElement, { attributes: true, attributeFilter: ['class'] });
      window.addEventListener('load', report);
      if (document.querySelector('title')) new MutationObserver(report).observe(document.querySelector('title'), { childList: true, subtree: true });
      report();
    })();
    """
    return WKUserScript(source: source, injectionTime: .atDocumentEnd, forMainFrameOnly: true)
}

/// 在首帧应用原生保存的主题，消除重新打开窗口时的主题闪烁。
func webStateBootstrapScript(store: WindowStateStore, origin: String) -> WKUserScript {
    let source = """
    (function() {
      if (location.origin !== \(javascriptString(origin))) return;
      try {
        var theme = \(javascriptString(store.webTheme ?? ""));
        if (theme === 'dark' || theme === 'light') {
          localStorage.setItem('columba-webui.theme', theme);
          document.documentElement.classList.toggle('dark', theme === 'dark');
        }
      } catch (e) {}
    })();
    """
    return WKUserScript(source: source, injectionTime: .atDocumentStart, forMainFrameOnly: true)
}

/// 解析 CSS 颜色("rgb(r, g, b)" / "rgba(r, g, b, a)")
func cssColor(_ text: String) -> NSColor? {
    let pattern = #"rgba?\(\s*([0-9.]+)[,\s]+([0-9.]+)[,\s]+([0-9.]+)(?:[,\s/]+([0-9.]+))?\s*\)"#
    guard let regex = try? NSRegularExpression(pattern: pattern),
          let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) else {
        return nil
    }
    func double(_ index: Int) -> Double? {
        guard let range = Range(match.range(at: index), in: text) else { return nil }
        return Double(text[range])
    }
    guard let red = double(1), let green = double(2), let blue = double(3) else { return nil }
    let alpha = double(4) ?? 1
    return NSColor(srgbRed: CGFloat(red / 255), green: CGFloat(green / 255),
                   blue: CGFloat(blue / 255), alpha: CGFloat(alpha))
}

// MARK: - 应用主体

/// 标题栏区域的透明拖拽条:不绘制任何内容,按住它拖动窗口。
/// 显式调用 performDrag 而非依赖 mouseDownCanMoveWindow 声明,
/// 后者在 WKWebView 覆盖同区域时不可靠。
final class DragTitlebarView: NSView {
    override func mouseDown(with event: NSEvent) {
        if event.clickCount == 2 {
            switch UserDefaults.standard.string(forKey: "AppleActionOnDoubleClick") {
            case "Minimize": window?.performMiniaturize(nil)
            case "None": break
            default: window?.performZoom(nil)
            }
        } else { window?.performDrag(with: event) }
    }
}

/// 菜单栏弹出菜单顶部的信息部件：品牌与运行状态、面板地址、最近 24 小时请求脉冲曲线与摘要。
/// 曲线与 WebUI 面板的实时脉搏同用瑰梅红；视图实例常驻并保留上次内容，
/// 状态由 AppDelegate 在状态变化时刷新，曲线数据在 menuWillOpen 时异步更新。
final class PulseMenuView: NSView {
    static let windowHours = 24
    static let slotCount = 96
    static let bucketMinutes = 15
    static let width: CGFloat = 264
    static let height: CGFloat = 100

    private var stateDot = NSColor.systemGray
    private var stateText = "已停止"
    private var subtitle = ""
    private var summary = ""
    private var slots: [Int] = []

    /// 深浅色菜单分别取色：浅色用面板同款瑰梅红（#DC185D，webui --primary），
    /// 深色提亮降饱和，避免高饱和玫红在暗背景上显得刺眼。
    private var tint: NSColor {
        let dark = effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
        return dark ? NSColor(srgbRed: 0.914, green: 0.388, blue: 0.596, alpha: 1)   // #E96398
                    : NSColor(srgbRed: 0.863, green: 0.094, blue: 0.403, alpha: 1)   // #DC185D
    }

    override func viewDidChangeEffectiveAppearance() {
        needsDisplay = true
    }

    override var intrinsicContentSize: NSSize { NSSize(width: Self.width, height: Self.height) }

    func update(stateDot: NSColor, stateText: String, subtitle: String, summary: String, slots: [Int]) {
        self.stateDot = stateDot
        self.stateText = stateText
        self.subtitle = subtitle
        self.summary = summary
        self.slots = slots
        setAccessibilityElement(true)
        setAccessibilityLabel("Columba，\(stateText)，\(subtitle)。\(summary)")
        needsDisplay = true
    }

    override func draw(_ dirtyRect: NSRect) {
        let inset: CGFloat = 14
        let truncate = NSMutableParagraphStyle()
        truncate.lineBreakMode = .byTruncatingTail

        // 第一行：标题与运行状态（品牌由正上方的菜单栏图标承担，部件内不再重复）
        let titleFont = NSFont.systemFont(ofSize: 13, weight: .semibold)
        let line1Y = bounds.height - 20
        ("Columba" as NSString).draw(at: NSPoint(x: inset, y: line1Y),
            withAttributes: [.font: titleFont, .foregroundColor: NSColor.labelColor])
        let stateAttrs: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 11, weight: .medium),
                                                         .foregroundColor: NSColor.secondaryLabelColor]
        let stateSize = stateText.size(withAttributes: stateAttrs)
        let dotSide: CGFloat = 7
        let dotX = bounds.width - inset - stateSize.width - dotSide - 4
        stateDot.setFill()
        NSBezierPath(ovalIn: NSRect(x: dotX, y: line1Y + 0.5, width: dotSide, height: dotSide)).fill()
        (stateText as NSString).draw(at: NSPoint(x: dotX + dotSide + 4, y: line1Y), withAttributes: stateAttrs)

        // 面积图 + 摘要 + 地址（地址最次要，沉底）
        drawPulse(in: NSRect(x: inset, y: 44, width: max(bounds.width - inset * 2, 0), height: 28))
        (summary as NSString).draw(at: NSPoint(x: inset, y: 30),
            withAttributes: [.font: NSFont.systemFont(ofSize: 11),
                             .foregroundColor: NSColor.secondaryLabelColor,
                             .paragraphStyle: truncate])
        (subtitle as NSString).draw(at: NSPoint(x: inset, y: 12),
            withAttributes: [.font: NSFont.systemFont(ofSize: 11),
                             .foregroundColor: NSColor.secondaryLabelColor,
                             .paragraphStyle: truncate])
    }

    private func drawPulse(in chart: NSRect) {
        NSColor.separatorColor.setFill()
        NSRect(x: chart.minX, y: chart.minY, width: chart.width, height: 1).fill()
        guard !slots.isEmpty, let peak = slots.max(), peak > 0 else { return }
        // sqrt 比例压缩突发峰值，小流量时段的起伏也可见；平滑剪影闭合后做纵向渐变填充，
        // 稀疏突发的请求量呈现为柔和的山丘而非针状。顶部留 3pt 防平滑过冲。
        let sqrtPeak = sqrt(CGFloat(peak))
        let step = chart.width / CGFloat(max(slots.count - 1, 1))
        let usable = chart.height - 3
        let points: [NSPoint] = slots.enumerated().map { index, value in
            NSPoint(x: chart.minX + CGFloat(index) * step,
                    y: chart.minY + 1 + sqrt(CGFloat(value)) / sqrtPeak * usable)
        }
        let curve = NSBezierPath()
        curve.move(to: points[0])
        if points.count == 1 { curve.line(to: points[0]) }
        for i in 1..<points.count {
            let p0 = points[max(i - 2, 0)], p1 = points[i - 1], p2 = points[i], p3 = points[min(i + 1, points.count - 1)]
            curve.curve(to: p2,
                        controlPoint1: NSPoint(x: p1.x + (p2.x - p0.x) / 6, y: p1.y + (p2.y - p0.y) / 6),
                        controlPoint2: NSPoint(x: p2.x - (p3.x - p1.x) / 6, y: p2.y - (p3.y - p1.y) / 6))
        }
        // 渐变填充剪影；山丘轮廓再以细描边勾形，避免在菜单材质上淡到不可见
        let area = curve.copy() as! NSBezierPath
        area.line(to: NSPoint(x: chart.maxX, y: chart.minY))
        area.line(to: NSPoint(x: chart.minX, y: chart.minY))
        area.close()
        NSGradient(colors: [tint.withAlphaComponent(0.65), tint.withAlphaComponent(0.10)])?.draw(in: area, angle: -90)
        tint.setStroke()
        curve.lineWidth = 1.5
        curve.lineCapStyle = .round
        curve.lineJoinStyle = .round
        curve.stroke()
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKDownloadDelegate, WKScriptMessageHandler, NSWindowDelegate, WKUIDelegate, NSMenuDelegate, NSMenuItemValidation {
    var panelDownloads: [ObjectIdentifier: PanelDownload] = [:]
    var cancellingPanelDownloads: [ObjectIdentifier: PanelDownload] = [:]
    var panelDownloadMessage = ""
    #if NATIVE_TESTS
    var panelDestinationReplies = 0
    #endif

    // The tray supervisor never constructs WebKit. Closing the panel exits its process.
    let isPanelProcess = CommandLine.arguments.contains("--webui-process")
    var usesPanelProcess: Bool {
        #if NATIVE_TESTS
        return !isPanelProcess && NSApp.delegate === self
        #else
        return !isPanelProcess
        #endif
    }
    var panelProcess: Process?
    var panelBridge: PanelBridge?
    var panelStopDeadline: DispatchWorkItem?
    var panelClosing = false
    var reopenPanelAfterExit = false
    var panelReady = false
    var panelBackendGeneration = ""
    private var instanceLock: Int32 = -1
    private var healthTimer: Timer?
    var window: NSWindow!
    var webView: WKWebView!
    var overlay: NSView!
    var overlaySpinner: NSProgressIndicator!
    var overlayLabel: NSTextField!
    var overlayButton: NSButton!
    var statusItem: NSStatusItem!
    var restartItem: NSMenuItem!
    var pulseItem: NSMenuItem!
    var pulseView: PulseMenuView!
    var pulseFetchGeneration = 0
    var pulseSlots: [Int] = []
    var pulseSummaryText = "最近 24 小时"
    var reloadPanelItem: NSMenuItem!

    var config: PanelConfig!
    var backend: Process?
    var backendHost: String?   // 后端实际监听地址,启动时锁定(config 之后被手改也不会错位)
    var backendPort = 0
    var userStopping = false
    var restartCount = 0
    var backendState: BackendState = .stopped
    var lastBackendError: String?
    var launchedAtLogin = CommandLine.arguments.contains("--login") || CommandLine.arguments.contains("--background")
    var terminating = false
    private var terminationReplyPending = false
    private var terminationCleanupDeadline: DispatchWorkItem?
    #if NATIVE_TESTS
    var terminationRepliesForTests = 0
    #endif
    var restartWork: DispatchWorkItem?
    var healthInFlight = false
    var startupTime = Date()
    var backendReachedHealthy = false
    var healthySince: Date?
    var backendLogHandle: FileHandle?
    var backendInputPipe: Pipe?
    var backendStopDeadline: DispatchWorkItem?
    var backendGeneration = UUID()
    var notificationSettingsItem: NSMenuItem!
    var loginSettingsItem: NSMenuItem!
    var lastErrorItem: NSMenuItem!
    var overlayHelp: NSStackView!
    var healthFailedSince: Date?
    var recoveryTerminating = false
    var notificationDenied = false
    var timers: [Timer] = []
    var panelLoaded = false
    var loadedPort = 0
    var currentVersion = readBundledVersion()
    let windowState = WindowStateStore()
    let launchAtLogin = LaunchAtLoginManager()
    let notifications = NotificationManager()
    var signalSources: [DispatchSourceSignal] = []
    var panelReadinessInFlight = false
    /// 重启等待旧后端退出，随后由退出回调启动新进程。
    var pendingBackendRestart = false

    let healthSession: URLSession = {
        let config = URLSessionConfiguration.ephemeral
        config.connectionProxyDictionary = [:]
        config.timeoutIntervalForRequest = 3
        return URLSession(configuration: config)
    }()
    let apiSession: URLSession = {
        let config = URLSessionConfiguration.ephemeral
        config.connectionProxyDictionary = [:]
        config.timeoutIntervalForRequest = 15
        return URLSession(configuration: config)
    }()

    /// 当前可用的 API 地址:后端在跑时用其实际监听地址(启动时锁定),
    /// 已停止时用配置地址(下次启动生效)
    private var apiBaseURL: String {
        let host = backendPort != 0 ? (backendHost ?? config.host) : config.host
        let port = backendPort != 0 ? backendPort : config.port
        return panelOrigin(host: host, port: port)
    }

    // MARK: 生命周期

    func applicationDidFinishLaunching(_ notification: Notification) {
        let event = NSAppleEventManager.shared().currentAppleEvent
        launchedAtLogin = launchedAtLogin || event?.paramDescriptor(forKeyword: keyAEPropData)?.enumCodeValue == keyAELaunchedAsLogInItem
        if isPanelProcess { launchPanelProcess(); return }
        guard ensureSingleInstance() else { requestTermination(); return }
        #if NATIVE_TESTS
        notifications.enabled = false
        #endif
        config = loadOrCreateConfig() ?? PanelConfig()
        appLogger.info("Launching Columba \(self.currentVersion, privacy: .public), background: \(self.launchedAtLogin)")
        notifications.onOpen = { [weak self] in self?.showMainWindow() }
        NSApp.setActivationPolicy(.accessory)
        buildMainMenu()
        buildStatusItem()
        if !launchedAtLogin { showMainWindow() }
        #if !NATIVE_TESTS
        do { try launchAtLogin.reconcile() }
        catch { appLogger.error("Login item reconciliation: \(error.localizedDescription, privacy: .public)") }
        #endif
        refreshPreferencesUI()
        startBackend()
        scheduleTimers()
        installSignalHandlers()
        NotificationCenter.default.addObserver(self, selector: #selector(screenLayoutChanged), name: NSApplication.didChangeScreenParametersNotification, object: nil)
        NSWorkspace.shared.notificationCenter.addObserver(self, selector: #selector(wakeFromSleep), name: NSWorkspace.didWakeNotification, object: nil)
    }

    func applicationSupportsSecureRestorableState(_ app: NSApplication) -> Bool { true }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        showMainWindow()
        return true
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if terminating { return .terminateLater }
        terminating = true
        restartWork?.cancel()
        healthTimer?.invalidate()
        healthTimer = nil
        timers.forEach { $0.invalidate() }
        timers.removeAll()
        apiSession.invalidateAndCancel()
        healthSession.invalidateAndCancel()
        cancelPanelDownloads(reason: "应用正在退出，未完成的导出已取消。")
        saveWindowState()
        stopPanelProcess()
        if let process = backend {
            if process.isRunning {
                userStopping = true
                backendState = .stopping
                refreshStatusUI()
                showOverlay(text: "正在停止服务并保存用量记录…", spinning: true)
                requestBackendExit(process)
            } else { backendDidExit(process) }
        }
        guard !terminationResourcesReleased else { return .terminateNow }
        terminationReplyPending = true
        let deadline = DispatchWorkItem { [weak self] in
            guard let self, self.terminationReplyPending else { return }
            appLogger.error("Termination cleanup deadline reached; discarding only owned download files")
            if let process = self.backend, process.isRunning { kill(process.processIdentifier, SIGKILL) }
            if let process = self.panelProcess, process.isRunning { kill(process.processIdentifier, SIGKILL) }
            self.discardTerminationDownloads()
            self.replyToTermination()
        }
        terminationCleanupDeadline = deadline
        DispatchQueue.main.asyncAfter(deadline: .now() + (isPanelProcess ? 4 : 16), execute: deadline)
        return .terminateLater
    }

    private var terminationResourcesReleased: Bool {
        backend == nil && panelProcess == nil && panelDownloads.isEmpty && cancellingPanelDownloads.isEmpty
    }

    func finishTerminationIfReady() {
        guard terminating, terminationReplyPending, terminationResourcesReleased else { return }
        replyToTermination()
    }

    private func replyToTermination() {
        terminationReplyPending = false
        terminationCleanupDeadline?.cancel()
        terminationCleanupDeadline = nil
        #if NATIVE_TESTS
        terminationRepliesForTests += 1
        // The integration suite invokes the real delegate without installing it on NSApp.
        if NSApp.delegate !== self { return }
        #endif
        NSApp.reply(toApplicationShouldTerminate: true)
    }

    private func discardTerminationDownloads() {
        for pending in cancellingPanelDownloads.values { pending.removeTemporaryFile() }
        cancellingPanelDownloads.removeAll()
    }

    func applicationWillTerminate(_ notification: Notification) {
        terminationCleanupDeadline?.cancel()
        panelStopDeadline?.cancel()
        panelBridge?.close()
        panelBridge = nil
        if instanceLock >= 0 { close(instanceLock); instanceLock = -1 }
        discardTerminationDownloads()
        backendStopDeadline?.cancel()
        try? backendInputPipe?.fileHandleForWriting.close()
        try? backendLogHandle?.close()
        window?.close()
        NotificationCenter.default.removeObserver(self)
        NSWorkspace.shared.notificationCenter.removeObserver(self)
        DistributedNotificationCenter.default().removeObserver(self)
        signalSources.forEach { $0.cancel() }
    }

    func requestTermination() {
        // terminateLater runs a nested AppKit loop. Enter it from the run loop,
        // so main-queue process/download completions can still finish cleanup.
        RunLoop.main.perform(inModes: [.common]) { [weak self] in
            guard let self, !self.terminating else { return }
            NSApp.terminate(nil)
        }
    }

    @objc func quit(_ sender: Any?) {
        if isPanelProcess { panelBridge?.send(PanelMessage(type: "action", action: "quit")) }
        else { requestTermination() }
    }
    @objc private func wakeFromSleep() { pollHealth() }
    @objc func screenLayoutChanged() { ensureWindowVisible() }

    // MARK: 单实例

    private var reopenNotification: Notification.Name {
        Notification.Name((Bundle.main.bundleIdentifier ?? "Columba") + ".showPanel")
    }

    private func ensureSingleInstance() -> Bool {
        do { try FileManager.default.createDirectory(atPath: dataDirPath, withIntermediateDirectories: true) }
        catch { appLogger.error("Cannot create application data directory: \(error.localizedDescription, privacy: .public)"); return false }
        instanceLock = open(dataDirPath + "/native.lock", O_CREAT | O_RDWR | O_CLOEXEC, 0o600)
        guard instanceLock >= 0 else { return false }
        guard flock(instanceLock, LOCK_EX | LOCK_NB) == 0 else {
            close(instanceLock)
            instanceLock = -1
            if !launchedAtLogin {
                DistributedNotificationCenter.default().postNotificationName(reopenNotification, object: nil, userInfo: nil, deliverImmediately: true)
            }
            return false
        }
        DistributedNotificationCenter.default().addObserver(self, selector: #selector(showMainWindow), name: reopenNotification, object: nil)
        return true
    }

    // MARK: 主窗口与界面

    private static let titleBarHeight: CGFloat = 28

    /// 最小主菜单:编辑菜单项是 WKWebView 复制/粘贴/全选等快捷键的依赖
    /// (标准 selector 不设 target,交由响应链分发到当前第一响应者)
    func buildMainMenu() {
        let mainMenu = NSMenu()

        let appMenuItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "关于 Columba", action: #selector(showAbout), keyEquivalent: "")
        addAction(appMenu, "偏好设置…", #selector(showPreferences), ",")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "隐藏 Columba", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        appMenu.addItem(withTitle: "隐藏其他", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "")
        appMenu.addItem(withTitle: "显示全部", action: #selector(NSApplication.unhideAllApplications(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "退出 Columba", action: #selector(quit), keyEquivalent: "q")
        appMenuItem.submenu = appMenu
        mainMenu.addItem(appMenuItem)

        let editMenuItem = NSMenuItem()
        let editMenu = NSMenu(title: "编辑")
        editMenu.addItem(withTitle: "撤销", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "重做", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: "剪切", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "复制", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editMenuItem.submenu = editMenu
        mainMenu.addItem(editMenuItem)

        let windowMenuItem = NSMenuItem()
        let windowMenu = NSMenu(title: "窗口")
        windowMenu.addItem(withTitle: "最小化", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "缩放", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        windowMenu.addItem(withTitle: "关闭窗口", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        let fullScreen = windowMenu.addItem(withTitle: "进入全屏幕", action: #selector(NSWindow.toggleFullScreen(_:)), keyEquivalent: "f")
        fullScreen.keyEquivalentModifierMask = [.command, .control]
        addAction(windowMenu, "显示主窗口", #selector(showMainWindow as () -> Void), "0")
        NSApp.windowsMenu = windowMenu
        windowMenuItem.submenu = windowMenu
        mainMenu.addItem(windowMenuItem)

        let viewMenuItem = NSMenuItem()
        let viewMenu = NSMenu(title: "视图")
        reloadPanelItem = viewMenu.addItem(withTitle: "重新加载面板", action: #selector(reloadPanel), keyEquivalent: "r")
        reloadPanelItem.target = self
        addAction(viewMenu, "在浏览器中打开面板", #selector(openPanelInBrowser), "b", [.command, .shift])
        addAction(viewMenu, "复制面板地址", #selector(copyPanelURL), "l", [.command, .shift])
        addAction(viewMenu, "复制 API 地址", #selector(copyAPIURL), "c", [.command, .option])
        addAction(viewMenu, "启动服务", #selector(restartBackend), "s", [.command, .option])
        viewMenuItem.submenu = viewMenu
        mainMenu.addItem(viewMenuItem)

        for menu in [appMenu, viewMenu, windowMenu] { menu.delegate = self }
        appMenu.items.first?.target = self
        appMenu.items.last?.target = self
        NSApp.mainMenu = mainMenu
    }

    func configureWebScripts() {
        guard let webView else { return }
        let content = webView.configuration.userContentController
        content.removeAllUserScripts()
        content.addUserScript(webStateBootstrapScript(store: windowState, origin: apiBaseURL))
        content.addUserScript(themeReporterScript())
    }

    private func initialWindowRect() -> NSRect {
        let visible = NSScreen.main?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
        let width = min(visible.width, min(max(940, visible.width * 0.82), 1400))
        let height = min(visible.height, min(max(600, visible.height * 0.82), 900))
        return NSRect(x: visible.midX - width / 2, y: visible.midY - height / 2, width: width, height: height)
    }

    private func ensureWindowVisible() {
        guard let window, !window.styleMask.contains(.fullScreen) else { return }
        let screens = NSScreen.screens.map(\.visibleFrame)
        let frame = WindowStateStore.fittedFrame(window.frame, screens: screens)
        window.minSize = NSSize(width: min(940, frame.width), height: min(600, frame.height))
        if frame != window.frame { window.setFrame(frame, display: true) }
    }

    func saveWindowState() {
        guard let window else { return }
        if !window.styleMask.contains(.fullScreen) { window.saveFrame(usingName: "ColumbaPanel") }
    }

    func buildWindow() {
        let rect = initialWindowRect()
        window = NSWindow(contentRect: rect,
                          styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                          backing: .buffered, defer: false)
        window.title = "Columba"
        window.minSize = NSSize(width: 940, height: 600)
        window.collectionBehavior = [.fullScreenPrimary]
        // 关闭窗口时不要释放 NSWindow:Swift 强引用无法感知 AppKit 的额外 release,
        // 否则窗口对象变成悬垂指针,再次「显示主窗口」会崩溃
        window.isReleasedWhenClosed = false
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.backgroundColor = .windowBackgroundColor
        window.delegate = self
        // 记住窗口位置;首次(重建)若无历史位置则居中
        if UserDefaults.standard.string(forKey: "NSWindow Frame ColumbaPanel") == nil {
            window.center()
        }
        window.setFrameAutosaveName("ColumbaPanel")
        window.setFrameUsingName("ColumbaPanel", force: true)
        ensureWindowVisible()

        let content = NSView(frame: NSRect(origin: .zero, size: window.contentView!.bounds.size))

        let webConfig = WKWebViewConfiguration()
        // 首次手动登录后保留 WebUI 自己写入的登录态，关窗仍销毁 WebView。
        webConfig.websiteDataStore = .default()
        let userContent = WKUserContentController()
        userContent.add(self, name: "theme")
        webConfig.userContentController = userContent
        webView = WKWebView(frame: .zero, configuration: webConfig)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        configureWebScripts()
        webView.setAccessibilityLabel("Columba 管理面板")
        content.addSubview(webView)

        let dragStrip = DragTitlebarView()
        dragStrip.setAccessibilityLabel("窗口标题栏，可拖动以移动窗口")
        content.addSubview(dragStrip)


        overlay = NSView()
        overlay.wantsLayer = true
        content.addSubview(overlay)
        overlaySpinner = NSProgressIndicator()
        overlaySpinner.style = .spinning
        overlayLabel = NSTextField(wrappingLabelWithString: "正在启动本地后端…")
        overlayLabel.font = .systemFont(ofSize: 14)
        overlayLabel.textColor = .secondaryLabelColor
        overlayLabel.alignment = .center
        overlayLabel.isSelectable = true
        overlayButton = NSButton(title: "重试", target: self, action: #selector(retryStartup))
        overlayButton.bezelStyle = .rounded
        let logs = NSButton(title: "查看运行日志", target: self, action: #selector(openLog))
        let folder = NSButton(title: "打开数据文件夹", target: self, action: #selector(openDataFolder))
        logs.bezelStyle = .rounded
        folder.bezelStyle = .rounded
        overlayHelp = NSStackView(views: [logs, folder])
        overlayHelp.spacing = 12
        let overlayStack = NSStackView(views: [overlaySpinner, overlayLabel, overlayButton, overlayHelp])
        overlayStack.orientation = .vertical
        overlayStack.alignment = .centerX
        overlayStack.spacing = 18
        overlay.addSubview(overlayStack)
        for view in [webView!, dragStrip, overlay!, overlayStack] {
            view.translatesAutoresizingMaskIntoConstraints = false
        }
        NSLayoutConstraint.activate([
            dragStrip.topAnchor.constraint(equalTo: content.topAnchor),
            dragStrip.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            dragStrip.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            dragStrip.heightAnchor.constraint(equalToConstant: Self.titleBarHeight),
            webView.topAnchor.constraint(equalTo: content.topAnchor),
            webView.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            webView.bottomAnchor.constraint(equalTo: content.bottomAnchor),
            overlay.leadingAnchor.constraint(equalTo: webView.leadingAnchor),
            overlay.trailingAnchor.constraint(equalTo: webView.trailingAnchor),
            overlay.topAnchor.constraint(equalTo: webView.topAnchor),
            overlay.bottomAnchor.constraint(equalTo: webView.bottomAnchor),
            overlayStack.centerXAnchor.constraint(equalTo: overlay.centerXAnchor),
            overlayStack.centerYAnchor.constraint(equalTo: overlay.centerYAnchor),
            overlayStack.widthAnchor.constraint(lessThanOrEqualTo: overlay.widthAnchor, multiplier: 0.85),
            overlayLabel.widthAnchor.constraint(lessThanOrEqualToConstant: 580),
            overlaySpinner.widthAnchor.constraint(equalToConstant: 32),
            overlaySpinner.heightAnchor.constraint(equalToConstant: 32),
        ])
        window.contentView = content
        let dark = windowState.webTheme.map { $0 == "dark" } ?? (window.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua)
        applyTheme(dark: dark, background: dark ? NSColor(white: 0.07, alpha: 1) : .windowBackgroundColor)
        renderBackendState()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        // 关窗口 = 轻量后台(菜单栏仍在),从菜单栏「显示主窗口」重新打开
        return true
    }

    @objc func showMainWindow() {
        if usesPanelProcess { showPanelProcess(); return }
        NSApp.setActivationPolicy(.regular)
        if let fresh = loadOrCreateConfig() { config = fresh }
        if window == nil { buildWindow() }
        // 打开窗口不覆盖“手动停止”的意图；用户可从窗口内启动按钮恢复服务。
        ensureWindowVisible()
        if window.isMiniaturized { window.deminiaturize(nil) }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        pollHealth()
    }

    func windowWillClose(_ notification: Notification) {
        guard let closing = notification.object as? NSWindow, closing === window else { return }
        saveWindowState()
        cancelPanelDownloads(reason: "主窗口已关闭，未完成的导出已取消。")
        NSApp.setActivationPolicy(.accessory)
        overlaySpinner?.stopAnimation(nil)
        webView?.stopLoading()
        webView?.navigationDelegate = nil
        webView?.uiDelegate = nil
        webView?.configuration.userContentController.removeScriptMessageHandler(forName: "theme")
        webView?.configuration.userContentController.removeAllUserScripts()
        webView?.removeFromSuperview()
        webView = nil
        window.contentView = nil
        window = nil
        overlay = nil
        overlayLabel = nil
        overlaySpinner = nil
        overlayButton = nil
        overlayHelp = nil
        panelLoaded = false
        loadedPort = 0
        refreshStatusUI()
        if isPanelProcess {
            panelBridge?.send(PanelMessage(type: "closing"))
            requestTermination()
        }
    }

    func showOverlay(text: String, spinning: Bool, retry: Bool = false) {
        guard window != nil else { return }
        overlayLabel.stringValue = text
        overlaySpinner.isHidden = !spinning
        if spinning { overlaySpinner.startAnimation(nil) } else { overlaySpinner.stopAnimation(nil) }
        overlayButton.isHidden = !retry
        overlayHelp.isHidden = !retry
        overlay.isHidden = false
    }

    private func hideOverlay() {
        guard window != nil else { return }
        overlay.isHidden = true
    }

    @objc func retryStartup() {
        if isPanelProcess {
            panelLoaded = false
            panelBridge?.send(PanelMessage(type: "action", action: "retry"))
            panelBridge?.send(PanelMessage(type: "hello"))
            return
        }
        restartCount = 0
        showOverlay(text: "正在启动本地后端…", spinning: true)
        if let process = backend {
            panelLoaded = false
            if backendState == .failed {
                recoveryTerminating = true
                setBackendState(.restarting)
                requestBackendExit(process)
            } else { pollHealth() }
        } else { startBackend() }
    }

    // MARK: 状态栏

    @discardableResult
    private func addAction(_ menu: NSMenu, _ title: String, _ action: Selector, _ key: String = "",
                           _ modifiers: NSEvent.ModifierFlags = .command) -> NSMenuItem {
        let item = menu.addItem(withTitle: title, action: action, keyEquivalent: key)
        item.target = self
        item.keyEquivalentModifierMask = modifiers
        return item
    }

    private func buildStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        let logo = Bundle.main.bundlePath + "/Contents/Resources/Columba.svg"
        let base = templateIcon(from: logo, size: 18)
            ?? NSImage(systemSymbolName: "shippingbox", accessibilityDescription: "Columba")?.withSymbolConfiguration(.init(pointSize: 15, weight: .medium))
        statusItem.button?.image = base
        let menu = NSMenu()
        menu.delegate = self
        // 菜单项视图不会按 intrinsicContentSize 自动布局，必须显式给定 frame。
        pulseView = PulseMenuView(frame: NSRect(x: 0, y: 0, width: PulseMenuView.width, height: PulseMenuView.height))
        pulseItem = NSMenuItem()
        pulseItem.view = pulseView
        menu.addItem(pulseItem)
        lastErrorItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
        menu.addItem(lastErrorItem)

        // —— 服务操作 ——
        menu.addItem(.separator())
        addAction(menu, "显示主窗口", #selector(showMainWindow as () -> Void), "0")
            .image = NSImage(systemSymbolName: "macwindow", accessibilityDescription: nil)
        restartItem = addAction(menu, "启动服务", #selector(restartBackend), "s", [.command, .option])
        restartItem.image = NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: nil)

        // —— 快速复制 ——
        menu.addItem(.separator())
        addAction(menu, "复制 API 地址", #selector(copyAPIURL), "c", [.command, .option])
            .image = NSImage(systemSymbolName: "link", accessibilityDescription: nil)

        menu.addItem(.separator())

        // —— 偏好与诊断 ——
        menu.addItem(.separator())
        addAction(menu, "偏好设置…", #selector(showPreferences), ",")
            .image = NSImage(systemSymbolName: "gearshape", accessibilityDescription: nil)
        // 开机启动与通知开关只在偏好设置里提供；这里仅保留系统要求人工批准/授权时的修复入口。
        loginSettingsItem = addAction(menu, "开机启动待批准：打开系统设置…", #selector(openLoginSettings))
        notificationSettingsItem = addAction(menu, "通知被系统禁用：打开系统设置…", #selector(openNotificationSettings))
        addAction(menu, "打开数据文件夹", #selector(openDataFolder))
            .image = NSImage(systemSymbolName: "folder", accessibilityDescription: nil)
        addAction(menu, "查看运行日志", #selector(openLog))
            .image = NSImage(systemSymbolName: "doc.text", accessibilityDescription: nil)
        addAction(menu, "关于 Columba", #selector(showAbout))
            .image = NSImage(systemSymbolName: "info.circle", accessibilityDescription: nil)
        menu.addItem(.separator())
        addAction(menu, "退出 Columba", #selector(quit), "q")
        statusItem.menu = menu
        refreshStatusUI()
        refreshPreferencesUI()
    }

    private var backendStateText: String {
        switch backendState {
        case .starting: "正在启动"
        case .running: "运行中"
        case .stopping: "正在停止"
        case .restarting: "正在重启（\(restartCount)/3）"
        case .failed: "服务异常"
        case .stopped: "已停止"
        }
    }

    private func stateDotColor(_ state: BackendState) -> NSColor {
        switch state {
        case .running: .labelColor
        case .starting, .stopping, .restarting: .secondaryLabelColor
        case .failed: .labelColor
        case .stopped: .systemGray
        }
    }

    func refreshStatusUI() {
        sendPanelState()
        guard statusItem != nil else { return }
        // 图标旁永不显示文字：状态经 tooltip 与菜单头部传达。
        let description = "Columba · \(backendStateText) · \(apiBaseURL)"
        statusItem.button?.title = ""
        statusItem.button?.toolTip = description
        statusItem.button?.setAccessibilityLabel(description)
        lastErrorItem.title = "最近错误：" + (lastBackendError ?? "").replacingOccurrences(of: "\n", with: " ")
        lastErrorItem.toolTip = lastBackendError
        lastErrorItem.isHidden = lastBackendError == nil
        restartItem.title = serviceActionTitle
        restartItem.isEnabled = canRestartBackend
        reloadPanelItem?.isEnabled = webView != nil && backendState == .running
        renderHeader()
    }




    /// 用当前状态与缓存的脉冲数据重绘菜单头部部件。
    private func renderHeader() {
        guard let pulseView else { return }
        let running = backendState == .running
        let hostPort = apiBaseURL.replacingOccurrences(of: "http://", with: "")
        pulseView.update(stateDot: stateDotColor(backendState),
                         stateText: backendStateText,
                         subtitle: "\(hostPort) · \(currentVersion)",
                         summary: running ? pulseSummaryText : "服务未运行",
                         slots: running ? pulseSlots : [])
    }

    private var serviceActionTitle: String {
        (backend != nil || (isPanelProcess && backendPort != 0)) ? "重启服务" : (backendState == .failed ? "重试启动服务" : "启动服务")
    }

    var canRestartBackend: Bool { !terminating && !recoveryTerminating && [.running, .stopped, .failed].contains(backendState) }

    func validateMenuItem(_ item: NSMenuItem) -> Bool {
        if terminating { return false }
        switch item.action {
        case #selector(restartBackend):
            item.title = serviceActionTitle
            return canRestartBackend
        case #selector(reloadPanel): return webView != nil && backendState == .running
        case #selector(openPanelInBrowser): return backendState == .running
        default: return item.action != nil
        }
    }

    func menuWillOpen(_ menu: NSMenu) {
        refreshStatusUI()
        refreshPreferencesUI()
        if menu == statusItem?.menu { refreshUsagePulse() }
    }

    private func refreshPreferencesUI() {
        loginSettingsItem?.isHidden = !launchAtLogin.requiresApproval
        notifications.refreshAuthorization { [weak self] denied in
            self?.notificationDenied = denied
            self?.notificationSettingsItem?.isHidden = !denied
        }
    }

    /// 菜单每次弹出时按需拉取最近 24 小时用量脉冲；失败静默保留上一次的图。
    private func refreshUsagePulse() {
        guard pulseView != nil else { return }
        guard backendState == .running else { renderHeader(); return }
        pulseFetchGeneration += 1
        let generation = pulseFetchGeneration
        let backendID = backendGeneration
        let now = Date()
        guard let url = usagePulseURL(base: apiBaseURL, now: now) else { return }
        let request = URLRequest(url: url)
        healthSession.dataTask(with: request) { [weak self] data, response, _ in
            guard (response as? HTTPURLResponse)?.statusCode == 200, let data,
                  let summary = parseUsagePulse(data) else { return }
            DispatchQueue.main.async {
                guard let self, generation == self.pulseFetchGeneration,
                      backendID == self.backendGeneration, self.backendState == .running, !self.terminating else { return }
                self.pulseSlots = usagePulseSlots(points: summary.points,
                                                  from: now.addingTimeInterval(-Double(PulseMenuView.windowHours) * 3600),
                                                  to: now, slots: PulseMenuView.slotCount)
                let requests = "最近 24 小时 · \(formatRequestCount(summary.windowRequests)) 次请求"
                self.pulseSummaryText = summary.windowTokens > 0
                    ? requests + " · \(formatTokenCount(summary.windowTokens)) tokens"
                    : requests
                self.refreshStatusUI()
            }
        }.resume()
    }

    @objc private func openLoginSettings() { launchAtLogin.openSystemSettings() }
    @objc private func openNotificationSettings() { notifications.openSystemSettings() }

    @objc private func reloadPanel() {
        guard let webView, backendState == .running else { return }
        webView.reload()
    }

    @objc private func openPanelInBrowser() {
        guard let url = URL(string: "\(apiBaseURL)/ui/") else { return }
        NSWorkspace.shared.open(url)
    }

    private func requestNotificationPermission() {
        notifications.requestPermission { [weak self] granted in
            guard let self else { return }
            self.refreshPreferencesUI()
            if !granted {
                let alert = NSAlert()
                alert.messageText = "通知已被系统关闭"
                alert.informativeText = "请在系统设置中允许 Columba 发送通知。菜单栏和窗口内仍会显示服务状态。"
                alert.addButton(withTitle: "打开系统设置")
                alert.addButton(withTitle: "稍后")
                NSApp.activate(ignoringOtherApps: true)
                if alert.runModal() == .alertFirstButtonReturn { self.notifications.openSystemSettings() }
            }
        }
    }

    @objc func showPreferences() {
        if isPanelProcess { panelBridge?.send(PanelMessage(type: "action", action: "preferences")); return }
        let alert = NSAlert()
        alert.messageText = "Columba 偏好设置"
        alert.informativeText = "开机启动时仅驻留菜单栏。关闭主窗口后服务继续运行。" +
            (notificationDenied ? "\n通知已被系统禁用，可在下方打开系统设置。" : "") +
            (launchAtLogin.requiresApproval ? "\n开机启动等待系统批准。" : "")
        let login = NSButton(checkboxWithTitle: "登录 macOS 时自动启动并驻留菜单栏", target: nil, action: nil)
        login.state = launchAtLogin.isEnabled ? .on : .off
        let notify = NSButton(checkboxWithTitle: "发送重要状态通知", target: nil, action: nil)
        notify.state = notifications.enabled ? .on : .off
        let settings = NSButton(title: "通知系统设置…", target: self, action: #selector(openNotificationSettings))
        settings.bezelStyle = .rounded
        var controls: [NSView] = [login, notify, settings]
        if launchAtLogin.requiresApproval {
            let approval = NSButton(title: "登录项系统设置…", target: self, action: #selector(openLoginSettings))
            approval.bezelStyle = .rounded
            controls.append(approval)
        }
        let stack = NSStackView(views: controls)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 10
        stack.translatesAutoresizingMaskIntoConstraints = false
        let container = NSView(frame: NSRect(x: 0, y: 0, width: 380, height: launchAtLogin.requiresApproval ? 138 : 104))
        container.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            stack.topAnchor.constraint(equalTo: container.topAnchor),
            stack.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])
        alert.accessoryView = container
        alert.addButton(withTitle: "保存")
        alert.addButton(withTitle: "取消")
        NSApp.activate(ignoringOtherApps: true)
        guard alert.runModal() == .alertFirstButtonReturn else { return }
        let wasEnabled = notifications.enabled
        do {
            try launchAtLogin.setEnabled(login.state == .on)
            windowState.set(login.state == .on, for: DefaultsKey.launchAtLogin)
            notifications.enabled = notify.state == .on
            windowState.set(notifications.enabled, for: DefaultsKey.notificationsEnabled)
            refreshPreferencesUI()
            if notifications.enabled && !wasEnabled { requestNotificationPermission() }
        } catch {
            self.alert("偏好设置保存失败:\n\(error.localizedDescription)")
        }
    }

    // MARK: 健康检查与面板加载

    private func scheduleTimers() {
        rescheduleHealthTimer()
        #if NATIVE_TESTS
        if ProcessInfo.processInfo.environment["COLUMBA_NATIVE_TRAY_TEST"] == "1" {
            let control = Timer(timeInterval: 0.1, repeats: true) { [weak self] _ in
                guard let self, let command = try? String(contentsOfFile: dataDirPath + "/tray-test-command", encoding: .utf8) else { return }
                try? FileManager.default.removeItem(atPath: dataDirPath + "/tray-test-command")
                switch command {
                case "show": self.showMainWindow()
                case "close": self.stopPanelProcess()
                case "quit": self.requestTermination()
                default: break
                }
            }
            timers.append(control)
            RunLoop.main.add(control, forMode: .common)
        }
        #endif
    }

    func rescheduleHealthTimer(for state: BackendState? = nil) {
        guard !isPanelProcess, !terminating else { return }
        let current = state ?? backendState
        if backend == nil && (current == .stopped || current == .failed) {
            healthTimer?.invalidate()
            healthTimer = nil
            return
        }
        let interval: TimeInterval = current == .running && panelProcess == nil && window == nil ? 30 : 3
        guard healthTimer?.timeInterval != interval else { return }
        healthTimer?.invalidate()
        let health = Timer(timeInterval: interval, repeats: true) { [weak self] _ in self?.pollHealth() }
        health.tolerance = interval == 30 ? 5 : 0.3
        healthTimer = health
        RunLoop.main.add(health, forMode: .common)
    }

    func loadPanelWhenHealthy() {
        guard backendState == .running, let webView else { return }
        if !panelLoaded || loadedPort != backendPort {
            if let url = URL(string: "\(apiBaseURL)/ui/#/overview") {
                panelLoaded = true
                panelReady = false
                loadedPort = backendPort
                configureWebScripts()
                showOverlay(text: "正在加载面板…", spinning: true)
                webView.load(URLRequest(url: url))
            }
        } else if !webView.isLoading { hideOverlay() }
    }

    func pollHealth() {
        if isPanelProcess { panelBridge?.send(PanelMessage(type: "hello")); return }
        guard let process = backend, process.isRunning, !healthInFlight, !userStopping, !terminating, !recoveryTerminating,
              let url = URL(string: "\(apiBaseURL)/health") else { return }
        let generation = backendGeneration
        healthInFlight = true
        healthSession.dataTask(with: url) { [weak self] _, response, error in
            DispatchQueue.main.async {
                guard let self, self.backendGeneration == generation, self.backend === process else { return }
                self.healthInFlight = false
                guard !self.userStopping, !self.terminating, !self.recoveryTerminating else { return }
                let ok = (response as? HTTPURLResponse)?.statusCode == 200
                if ok {
                    self.backendReachedHealthy = true
                    self.healthFailedSince = nil
                    let recovering = self.backendState == .restarting || self.backendState == .failed
                    if self.healthySince == nil { self.healthySince = Date() }
                    // 连续健康 60 秒才重置失败预算，避免启动即崩溃无限循环。
                    if Date().timeIntervalSince(self.healthySince!) >= 60 { self.restartCount = 0 }
                    self.setBackendState(.running)
                    if recovering {
                        self.notifications.requestAndSend(title: "Columba 已恢复", body: "服务已恢复，可以继续使用。", identifier: "backend-recovered")
                    }
                    self.loadPanelWhenHealthy()
                } else {
                    self.healthySince = nil
                    if self.healthFailedSince == nil { self.healthFailedSince = Date() }
                    // A live child may still be migrating; the 15-second watchdog is for an already-ready service.
                    if !self.backendReachedHealthy && Date().timeIntervalSince(self.startupTime) < backendStartupTimeout { return }
                    if self.backendState == .running || !self.backendReachedHealthy {
                        self.setBackendState(.failed, error: "服务暂时无法连接，正在检查恢复。\n\(error?.localizedDescription ?? "健康检查未通过")")
                    }
                    // 短暂网络/唤醒抖动可自行恢复；持续 15 秒不健康则重启本壳拥有的进程。
                    if Date().timeIntervalSince(self.healthFailedSince!) >= 15 {
                        self.recoveryTerminating = true
                        self.setBackendState(.restarting, error: "健康检查持续失败，正在重启服务。")
                        self.requestBackendExit(process)
                    }
                }
            }
        }.resume()
    }

    // MARK: - WKNavigationDelegate

    func isPanelOrigin(_ url: URL) -> Bool {
        guard let base = URL(string: apiBaseURL) else { return false }
        return url.scheme == base.scheme && url.host == base.host && url.port == base.port
    }

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url else { decisionHandler(.cancel); return }
        if action.shouldPerformDownload && (isPanelOrigin(url) || url.scheme == "blob") {
            decisionHandler(.download)
        } else if isPanelOrigin(url) && (url.path == "/ui" || url.path.hasPrefix("/ui/")) {
            decisionHandler(.allow)
        } else {
            if action.targetFrame == nil || action.targetFrame?.isMainFrame == true {
                if ["https", "http", "mailto"].contains(url.scheme ?? "") { NSWorkspace.shared.open(url) }
            }
            decisionHandler(.cancel)
        }
    }

    func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse,
                 decisionHandler: @escaping (WKNavigationResponsePolicy) -> Void) {
        decisionHandler(response.canShowMIMEType ? .allow : .download)
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if action.targetFrame == nil, let url = action.request.url,
           isPanelOrigin(url), url.path.hasPrefix("/ui/") { webView.load(action.request) }
        return nil
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        guard webView === self.webView else { return }
        panelLoaded = false
        showOverlay(text: "面板进程已退出，正在重新加载…", spinning: true)
        appLogger.error("WebKit content process terminated")
        pollHealth()
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        guard webView === self.webView else { return }
        if backendState == .running {
            hideOverlay()
            reportPanelReadiness()
        }
    }

    /// 面板加载失败(含连接被拒的 provisional 阶段):清掉"已加载"标记,
    /// 健康轮询确认后端恢复后会自动重载页面,无需用户手动重试。
    private func handlePanelLoadFailure(_ error: Error) {
        if (error as NSError).domain == NSURLErrorDomain && (error as NSError).code == NSURLErrorCancelled { return }
        panelLoaded = false
        showOverlay(text: "面板加载失败:\n\(error.localizedDescription)", spinning: false, retry: true)
        if isPanelProcess {
            DispatchQueue.main.asyncAfter(deadline: .now() + 3) { [weak self] in
                guard let self, self.window != nil, !self.terminating, !self.panelLoaded else { return }
                self.panelBridge?.send(PanelMessage(type: "hello"))
            }
        }
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        guard webView === self.webView else { return }
        handlePanelLoadFailure(error)
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        guard webView === self.webView else { return }
        handlePanelLoadFailure(error)
    }

    // MARK: - 主题同步(WKScriptMessageHandler)

    func userContentController(_ userContentController: WKUserContentController,
                               didReceive message: WKScriptMessage) {
        guard message.name == "theme", message.frameInfo.isMainFrame,
              let url = message.frameInfo.request.url, isPanelOrigin(url),
              let body = message.body as? [String: Any] else { return }
        let dark = body["dark"] as? Bool ?? false
        let background = (body["background"] as? String).flatMap(cssColor) ?? .windowBackgroundColor
        windowState.save(theme: dark ? "dark" : "light")
        if let title = body["title"] as? String, !title.isEmpty {
            window?.title = String(title.prefix(120))
        }
        applyTheme(dark: dark, background: background)
    }

    /// 用页面主题为窗口背景/遮罩着色,使原生区域与网页无缝衔接
    private func applyTheme(dark: Bool, background: NSColor) {
        guard window != nil else { return }
        window.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        window.backgroundColor = background
        overlay.layer?.backgroundColor = background.cgColor
        webView.underPageBackgroundColor = background
    }

    // MARK: 菜单动作

    @objc private func copyPanelURL() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString("\(apiBaseURL)/ui/", forType: .string)
    }

    @objc private func copyAPIURL() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(apiBaseURL, forType: .string)
    }


    @objc private func openDataFolder() {
        NSWorkspace.shared.open(URL(fileURLWithPath: dataDirPath))
    }

    @objc private func openLog() {
        NSWorkspace.shared.open(URL(fileURLWithPath: logPath))
    }

    @objc private func showAbout() {
        alert("Columba for macOS\n当前版本 \(currentVersion)\n本地模型聚合与费用统计。")
    }

    func alert(_ text: String) {
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.messageText = text
        alert.addButton(withTitle: "好")
        alert.runModal()
    }
}

// MARK: - 入口

// Pipe writes report EPIPE instead of terminating the owner when its peer exits.
signal(SIGPIPE, SIG_IGN)


#if NATIVE_TESTS
// Test hooks are compiled out of the shipped app. All fixture data/preferences have a unique domain.
extension AppDelegate {
    func prepareForTests() {
        config = loadOrCreateConfig() ?? PanelConfig()
        notifications.enabled = false
        buildMainMenu()
        buildStatusItem()
    }
    func pollForTests() { pollHealth() }
    func retryForTests() { retryStartup() }
    func ageHealthFailureForTests() { healthFailedSince = Date().addingTimeInterval(-16) }
}
if ProcessInfo.processInfo.environment["COLUMBA_NATIVE_APP_TEST"] == "1" {
    let app = NSApplication.shared
    let delegate = AppDelegate()
    app.delegate = delegate
    app.setActivationPolicy(.accessory)
    app.run()
} else {
    MainActor.assumeIsolated { NativeTests.run() }
}
#else
let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()

#endif
