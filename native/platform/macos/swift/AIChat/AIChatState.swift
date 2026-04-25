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

    private static let visibleKey = "ai_chat.visible"
    private static let modeKey = "ai_chat.mode"

    private init() {
        self.isVisible = UserDefaults.standard.bool(forKey: Self.visibleKey)
        let raw = UserDefaults.standard.string(forKey: Self.modeKey) ?? ""
        self.mode = Mode(rawValue: raw) ?? .docked
    }

    private func persist() {
        UserDefaults.standard.set(isVisible, forKey: Self.visibleKey)
        UserDefaults.standard.set(mode.rawValue, forKey: Self.modeKey)
    }
}
