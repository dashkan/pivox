import Foundation
import Observation

/// Shared visibility + mode state for the AI Chat surface. The
/// SwiftUI side reads `isVisible` and `mode` to decide whether to
/// render the inline panel; the AppDelegate reads them to manage
/// the detached `NSWindow`.
///
/// Persistence lives in UserDefaults under stable keys so a
/// previous session's mode + visibility can be restored on next
/// launch (after sign-in completes — chat is account-scoped).
@Observable
@MainActor
final class AIChatState {
    static let shared = AIChatState()

    enum Mode: String { case docked, detached }

    /// How the docked (inline) chat panel is laid out within the
    /// main window.
    ///
    ///   - `.float`: panel is a card overlaid on the canvas with
    ///     translucent material (Liquid Glass on macOS 26+) and a
    ///     drop shadow. Canvas keeps its full geometric width;
    ///     content peeks through behind the chat.
    ///   - `.push`: panel takes a fixed slice on the right of the
    ///     window; canvas resizes to make room. The Mac-conservative
    ///     pattern (Mail/Slack/Cursor/Xcode all push).
    ///
    /// User-controlled via the AI settings tab. Default is
    /// `.float` because canvas-cramping is the most common
    /// complaint on laptop-sized windows; users on big monitors
    /// who want side-by-side flip to `.push`.
    enum LayoutMode: String { case float, push }

    /// True when the chat surface should be on screen — inline if
    /// `mode == .docked`, in the detached window if
    /// `mode == .detached`.
    var isVisible: Bool {
        didSet { persist() }
    }

    /// Where the chat surface is rendered.
    var mode: Mode {
        didSet { persist() }
    }

    /// How the inline (docked) chat panel is laid out. Ignored
    /// when `mode == .detached`.
    var layoutMode: LayoutMode {
        didSet { persist() }
    }

    private static let visibleKey = "ai_chat.visible"
    private static let modeKey = "ai_chat.mode"
    private static let layoutModeKey = "ai_chat.layoutMode"

    private let defaults: UserDefaults

    /// Internal initializer — production code uses the
    /// `.shared` singleton (which constructs against
    /// `UserDefaults.standard`); tests pass a sandboxed
    /// `UserDefaults(suiteName:)` to verify persistence
    /// round-trips without polluting the real defaults database.
    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        self.isVisible = defaults.bool(forKey: Self.visibleKey)
        let modeRaw = defaults.string(forKey: Self.modeKey) ?? ""
        self.mode = Mode(rawValue: modeRaw) ?? .docked
        let layoutRaw = defaults.string(forKey: Self.layoutModeKey) ?? ""
        self.layoutMode = LayoutMode(rawValue: layoutRaw) ?? .float
    }

    private func persist() {
        defaults.set(isVisible, forKey: Self.visibleKey)
        defaults.set(mode.rawValue, forKey: Self.modeKey)
        defaults.set(layoutMode.rawValue, forKey: Self.layoutModeKey)
    }
}
