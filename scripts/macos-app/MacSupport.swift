import Cocoa
import ServiceManagement
import UserNotifications
import os

struct AppError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

enum DefaultsKey {
    static let launchAtLogin = "Columba.launchAtLogin"
    static let notificationsEnabled = "Columba.notificationsEnabled"
    static let webTheme = "Columba.webTheme"
}

enum BackendState: String {
    case stopped
    case starting
    case running
    case stopping
    case restarting
    case failed
}

// Upgrade-time database initialization runs before HTTP starts listening.
let backendStartupTimeout: TimeInterval = 180

/// 保存窗口位置之外的原生偏好；WebUI 登录态由 WebKit 数据存储管理。
final class WindowStateStore {
    private let defaults: UserDefaults
    private let defaultsOverride: UserDefaults?
    init(defaults: UserDefaults = .standard, overrides: UserDefaults? = nil) {
        self.defaults = defaults
        self.defaultsOverride = overrides
        defaults.removeObject(forKey: "Columba.lastRoute")
    }
    /// 读写顺序：测试覆盖 > 标准库。
    private var effective: UserDefaults { defaultsOverride ?? defaults }
    func get(_ key: String) -> Bool { effective.bool(forKey: key) }
    func set(_ value: Bool, for key: String) { defaults.set(value, forKey: key) }
    var webTheme: String? {
        guard let theme = defaults.string(forKey: DefaultsKey.webTheme), ["light", "dark"].contains(theme) else { return nil }
        return theme
    }
    func save(theme: String) {
        guard ["light", "dark"].contains(theme) else { return }
        defaults.set(theme, forKey: DefaultsKey.webTheme)
    }

    /// 以最大重叠屏幕为目标，完整放回工作区；也处理窗口只有一小条仍在屏内的情况。
    static func fittedFrame(_ frame: NSRect, screens: [NSRect]) -> NSRect {
        guard let fallback = screens.first else { return frame }
        let screen = screens.max {
            let a = $0.intersection(frame), b = $1.intersection(frame)
            return (a.isNull ? 0 : a.width * a.height) < (b.isNull ? 0 : b.width * b.height)
        } ?? fallback
        let target = screens.contains(where: { $0.intersects(frame) }) ? screen : fallback
        let width = min(frame.width, target.width), height = min(frame.height, target.height)
        return NSRect(x: min(max(frame.minX, target.minX), target.maxX - width),
                      y: min(max(frame.minY, target.minY), target.maxY - height), width: width, height: height)
    }
}

/// macOS 13+ 的注册状态以系统为准；12 的固定文件名避免路径变化后留下多个登录项。
final class LaunchAtLoginManager {
    private let bundleID = Bundle.main.bundleIdentifier ?? "local.columba.app"
    private var label: String { "\(bundleID).login" }
    private var launchAgentURL: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/LaunchAgents/\(label).plist")
    }
    var isEnabled: Bool {
        if #available(macOS 13.0, *) {
            return [.enabled, .requiresApproval].contains(SMAppService.mainApp.status)
        }
        return FileManager.default.fileExists(atPath: launchAgentURL.path)
    }
    var requiresApproval: Bool {
        if #available(macOS 13.0, *) { return SMAppService.mainApp.status == .requiresApproval }
        return false
    }

    static func legacyPayload(bundlePath: String, plistPath: String, label: String) -> [String: Any] {
        // 路径作为参数传递，不拼入 shell 源码。若应用已移走/卸载，下次登录自清理。
        let script = """
        if [ -x "$1/Contents/MacOS/Columba" ]; then
          exec /usr/bin/open -gj "$1" --args --login
        else
          /bin/rm -f "$2"
          /bin/launchctl remove "$3"
        fi
        """
        return ["Label": label, "ProgramArguments": ["/bin/sh", "-c", script, "columba-login", bundlePath, plistPath, label],
                "RunAtLoad": true, "KeepAlive": false, "ProcessType": "Interactive"]
    }

    func setEnabled(_ enabled: Bool) throws {
        if #available(macOS 13.0, *) {
            if enabled {
                if !isEnabled { try SMAppService.mainApp.register() }
            } else if SMAppService.mainApp.status != .notRegistered {
                try SMAppService.mainApp.unregister()
            }
            try removeLegacyAgent()
        } else if enabled {
            let payload = Self.legacyPayload(bundlePath: Bundle.main.bundlePath, plistPath: launchAgentURL.path, label: label)
            let data = try PropertyListSerialization.data(fromPropertyList: payload, format: .xml, options: 0)
            // 相同配置无需卸载/重载，避免重复打开应用。
            if (try? Data(contentsOf: launchAgentURL)) != data {
                try removeLegacyAgent()
                try FileManager.default.createDirectory(at: launchAgentURL.deletingLastPathComponent(), withIntermediateDirectories: true)
                try data.write(to: launchAgentURL, options: .atomic)
                do {
                    try runSystemTool("/bin/launchctl", ["bootstrap", "gui/\(getuid())", launchAgentURL.path])
                } catch {
                    try? FileManager.default.removeItem(at: launchAgentURL)
                    throw error
                }
            }
        } else {
            try removeLegacyAgent()
        }
        UserDefaults.standard.set(enabled, forKey: DefaultsKey.launchAtLogin)
    }

    /// 启动时只修复已有的旧版登录项，不擅自重新启用在系统设置中被用户关闭的注册。
    func reconcile() throws {
        guard FileManager.default.fileExists(atPath: launchAgentURL.path) else { return }
        if #available(macOS 13.0, *) {
            if UserDefaults.standard.bool(forKey: DefaultsKey.launchAtLogin) { try setEnabled(true) }
            else { try removeLegacyAgent() }
        } else {
            try setEnabled(UserDefaults.standard.bool(forKey: DefaultsKey.launchAtLogin))
        }
    }

    private func removeLegacyAgent() throws {
        guard FileManager.default.fileExists(atPath: launchAgentURL.path) else { return }
        // bootout 对尚未加载的 job 返回非零；只删除属于本应用固定 label 的文件。
        let service = "gui/\(getuid())/\(label)"
        if (try? runSystemTool("/bin/launchctl", ["print", service])) != nil {
            try runSystemTool("/bin/launchctl", ["bootout", service])
        }
        try FileManager.default.removeItem(at: launchAgentURL)
    }

    func openSystemSettings() {
        if #available(macOS 13.0, *) { SMAppService.openSystemSettingsLoginItems() }
    }
}

/// 同步工具只用于短小本地命令；参数不经过 shell 插值。
func runSystemTool(_ path: String, _ arguments: [String], timeout: TimeInterval = 30) throws {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: path)
    process.arguments = arguments
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice
    try process.run()
    let deadline = ProcessInfo.processInfo.systemUptime + timeout
    while process.isRunning && ProcessInfo.processInfo.systemUptime < deadline { usleep(100_000) }
    if process.isRunning {
        process.terminate()
        let termDeadline = ProcessInfo.processInfo.systemUptime + 2
        while process.isRunning && ProcessInfo.processInfo.systemUptime < termDeadline { usleep(100_000) }
        if process.isRunning { kill(process.processIdentifier, SIGKILL) }
        let killDeadline = ProcessInfo.processInfo.systemUptime + 2
        while process.isRunning && ProcessInfo.processInfo.systemUptime < killDeadline { usleep(100_000) }
        throw AppError(message: "\(URL(fileURLWithPath: path).lastPathComponent) 执行超时")
    }
    guard process.terminationStatus == 0 else {
        throw AppError(message: "\(URL(fileURLWithPath: path).lastPathComponent) 执行失败（\(process.terminationStatus)）")
    }
}

/// 统一 macOS 通知权限、发送与点击回调。默认允许重要状态通知，系统只在
/// 第一次发送前询问权限，用户也可从菜单栏关闭。
final class NotificationManager: NSObject, UNUserNotificationCenterDelegate {
    private let center = UNUserNotificationCenter.current()
    var enabled: Bool {
        get { UserDefaults.standard.object(forKey: DefaultsKey.notificationsEnabled) == nil || UserDefaults.standard.bool(forKey: DefaultsKey.notificationsEnabled) }
        set { UserDefaults.standard.set(newValue, forKey: DefaultsKey.notificationsEnabled) }
    }
    var onOpen: (() -> Void)?

    override init() {
        super.init()
        center.delegate = self
    }

    func requestAndSend(title: String, body: String, identifier: String, userInfo: [AnyHashable: Any] = [:]) {
        guard enabled else { return }
        center.getNotificationSettings { [weak self] settings in
            guard let self, self.enabled else { return }
            switch settings.authorizationStatus {
            case .authorized, .provisional:
                self.send(title: title, body: body, identifier: identifier, userInfo: userInfo)
            case .notDetermined:
                self.center.requestAuthorization(options: [.alert, .sound]) { [weak self] granted, _ in
                    guard granted else { return }
                    self?.send(title: title, body: body, identifier: identifier, userInfo: userInfo)
                }
            default:
                break
            }
        }
    }

    func refreshAuthorization(_ completion: @escaping (Bool) -> Void) {
        center.getNotificationSettings { settings in
            DispatchQueue.main.async { completion(settings.authorizationStatus == .denied) }
        }
    }

    /// 用户显式打开开关时申请权限；默认打开时仍等第一条重要通知。
    func requestPermission(_ completion: @escaping (Bool) -> Void) {
        center.requestAuthorization(options: [.alert, .sound]) { granted, _ in
            DispatchQueue.main.async { completion(granted) }
        }
    }

    func openSystemSettings() {
        let path: String
        if #available(macOS 13.0, *) { path = "x-apple.systempreferences:com.apple.Notifications-Settings.extension" }
        else { path = "x-apple.systempreferences:com.apple.preference.notifications" }
        guard let url = URL(string: path) else { return }
        NSWorkspace.shared.open(url)
    }

    private func send(title: String, body: String, identifier: String, userInfo: [AnyHashable: Any]) {
        guard enabled else { return }
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.sound = .default
        content.userInfo = userInfo
        center.add(UNNotificationRequest(identifier: identifier, content: content, trigger: nil))
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler(enabled ? [.banner, .list, .sound] : [])
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                didReceive response: UNNotificationResponse,
                                withCompletionHandler completionHandler: @escaping () -> Void) {
        DispatchQueue.main.async { [weak self] in self?.onOpen?() }
        completionHandler()
    }
}

let appLogger = Logger(subsystem: Bundle.main.bundleIdentifier ?? "local.columba.app", category: "app")

// An explicit HTTP proxy is shared by release checks and downloads; local API sessions stay direct.





func panelOrigin(host: String, port: Int) -> String {
    let local = host == "0.0.0.0" ? "127.0.0.1" : (host == "::" || host == "[::]" ? "::1" : host)
    let address = local.contains(":") && !local.hasPrefix("[") ? "[\(local)]" : local
    return "http://\(address):\(port)"
}

// MARK: - 菜单栏用量脉冲

struct UsagePulsePoint: Equatable {
    let time: Date
    let requests: Int
}

struct UsagePulseSummary: Equatable {
    let points: [UsagePulsePoint]
    let windowRequests: Int
    let windowTokens: Int
}

/// 解析 /api/admin/usage/pulse 响应（{ok, data:{points:[{t: Unix 毫秒, requests}], window:{requests, totalTokens}}}）。
func parseUsagePulse(_ data: Data) -> UsagePulseSummary? {
    struct Envelope: Decodable {
        struct Content: Decodable {
            struct Point: Decodable { let t: Double; let requests: Int }
            struct Window: Decodable { let requests: Int; let totalTokens: Int? }
            let points: [Point]?
            let window: Window?
        }
        let ok: Bool
        let data: Content?
    }
    guard let payload = try? JSONDecoder().decode(Envelope.self, from: data), payload.ok, let content = payload.data else { return nil }
    let points = (content.points ?? []).map { UsagePulsePoint(time: Date(timeIntervalSince1970: $0.t / 1000), requests: $0.requests) }
    let requests = content.window?.requests ?? points.reduce(0) { $0 + $1.requests }
    return UsagePulseSummary(points: points, windowRequests: requests, windowTokens: content.window?.totalTokens ?? 0)
}

/// 构造最近窗口的 pulse 请求地址。时间用 UTC 的 RFC3339（避免 '+' 被 Go query 解析成空格）。
func usagePulseURL(base: String, now: Date = Date(), windowHours: Int = 24, bucketMinutes: Int = 15) -> URL? {
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "en_US_POSIX")
    formatter.timeZone = TimeZone(identifier: "UTC")
    formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss'Z'"
    var components = URLComponents(string: "\(base)/api/admin/usage/pulse")
    components?.queryItems = [
        URLQueryItem(name: "bucketMinutes", value: String(bucketMinutes)),
        URLQueryItem(name: "from", value: formatter.string(from: now.addingTimeInterval(-Double(windowHours) * 3600))),
        URLQueryItem(name: "to", value: formatter.string(from: now)),
        URLQueryItem(name: "utcOffsetMinutes", value: String(TimeZone.current.secondsFromGMT(for: now) / 60)),
    ]
    return components?.url
}

/// 后端只返回非空时间桶；把稀疏点按时间填进固定数量的槽位（无数据为 0），超出窗口的点丢弃。
func usagePulseSlots(points: [UsagePulsePoint], from: Date, to: Date, slots: Int) -> [Int] {
    guard slots > 0, to > from else { return Array(repeating: 0, count: max(slots, 0)) }
    var filled = Array(repeating: 0, count: slots)
    let interval = to.timeIntervalSince(from) / Double(slots)
    for point in points {
        let index = Int(point.time.timeIntervalSince(from) / interval)
        guard filled.indices.contains(index) else { continue }
        filled[index] += point.requests
    }
    return filled
}

private let requestCountFormatter: NumberFormatter = {
    let formatter = NumberFormatter()
    formatter.numberStyle = .decimal
    return formatter
}()

func formatRequestCount(_ requests: Int) -> String {
    requestCountFormatter.string(from: NSNumber(value: requests)) ?? String(requests)
}

/// token 紧凑格式，与 WebUI 仪表盘一致：866k / 1.1M / 15M。
func formatTokenCount(_ tokens: Int) -> String {
    switch tokens {
    case 10_000_000...: "\(tokens / 1_000_000)M"
    case 1_000_000..<10_000_000: String(format: "%.1fM", Double(tokens) / 1_000_000)
    case 10_000..<1_000_000: "\(tokens / 1_000)k"
    case 1_000..<10_000: String(format: "%.1fk", Double(tokens) / 1_000)
    default: String(tokens)
    }
}

func persistPort(_ port: Int, at url: URL) throws {
    let data = try Data(contentsOf: url)
    guard var object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
        throw AppError(message: "配置文件必须是 JSON 对象")
    }
    let permissions = try FileManager.default.attributesOfItem(atPath: url.path)[.posixPermissions] ?? 0o600
    object["port"] = port
    try JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys]).write(to: url, options: .atomic)
    try FileManager.default.setAttributes([.posixPermissions: permissions], ofItemAtPath: url.path)
}
