import AppKit
import SwiftUI

/// Detached AI Chat window. Hosts `AIChatContainerView` in its own
/// `NSWindow` so the chat doesn't compete with the sidebar and main
/// canvas for horizontal space inside the primary window — useful
/// in the multi-monitor setups Pivox operators typically run.
///
/// The window lives across open/close cycles (`isReleasedWhenClosed
/// = false`) so the chat's SwiftUI state — the in-progress
/// conversation, message draft, ChatClient connection — survives a
/// red-traffic-light close. Reopening brings the same surface back.
@MainActor
final class AIChatWindowController: NSWindowController, NSWindowDelegate, NSToolbarDelegate {
    /// Fired when the user closes the window (red traffic light or
    /// ⌘W). The owner uses this to keep visibility state in sync.
    var onWindowClosed: (() -> Void)?

    /// UserDefaults key for the "keep on top" toggle so the user's
    /// last preference sticks across launches.
    private static let keepOnTopKey = "aiChat.keepOnTop"

    private var keepOnTopItem: NSToolbarItem?

    init() {
        let hosting = NSHostingController(rootView: AIChatContainerView())
        let initial = NSRect(x: 0, y: 0, width: 400, height: 700)
        let window = NSWindow(
            contentRect: initial,
            styleMask: [.titled, .closable, .resizable, .miniaturizable],
            backing: .buffered,
            defer: false)
        window.title = "Pivox AI"
        window.isReleasedWhenClosed = false
        window.contentView = hosting.view
        // Width bounds match the inline panel's bounds so the chat
        // looks the same in both modes — below ~340 the prompt
        // input crowds the action buttons; above ~640 the message
        // bubbles stretch into uncomfortable line lengths and the
        // window starts feeling like a primary surface rather than
        // an auxiliary one. Height has a sensible floor and
        // otherwise grows to fit the user's display.
        window.contentMinSize = NSSize(width: 340, height: 480)
        window.contentMaxSize = NSSize(
            width: 640,
            height: CGFloat.greatestFiniteMagnitude)

        // Persist + restore frame across launches via AppKit's stock
        // autosave mechanism.
        let autosaveName = NSWindow.FrameAutosaveName("PivoxAIChat")
        let restored = window.setFrameUsingName(autosaveName)
        window.setFrameAutosaveName(autosaveName)
        if !restored { window.center() }

        super.init(window: window)
        window.delegate = self
        installToolbar()
        applyKeepOnTop(UserDefaults.standard.bool(forKey: Self.keepOnTopKey))
    }

    required init?(coder: NSCoder) { fatalError("not implemented") }

    /// Bring the window forward, re-centering if a stale persisted
    /// frame would land it off-screen (monitor disconnect / display
    /// rearrangement).
    func show() {
        recenterIfOffScreen()
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    /// Ask the SwiftUI tree to focus the chat input. Used right
    /// after `show()` when the user opened the chat with intent to
    /// type immediately (toolbar mouse click, ⌘⇧A).
    func focusMessageInput() {
        NotificationCenter.default.post(name: .aiChatFocusRequested, object: nil)
    }

    func windowWillClose(_ notification: Notification) {
        onWindowClosed?()
    }

    // MARK: - Keep on top

    /// Apply the level + update the toolbar button image to reflect
    /// the current pin state. Called both when the user clicks the
    /// pin button and on init from the persisted default.
    private func applyKeepOnTop(_ pinned: Bool) {
        window?.level = pinned ? .floating : .normal
        UserDefaults.standard.set(pinned, forKey: Self.keepOnTopKey)
        // SF Symbol: filled pin when pinned, slashed when not.
        let symbol = pinned ? "pin.fill" : "pin.slash"
        keepOnTopItem?.image = NSImage(
            systemSymbolName: symbol,
            accessibilityDescription: pinned ? "Unpin window" : "Keep on top")
    }

    @objc private func toggleKeepOnTop(_ sender: Any?) {
        let nowPinned = (window?.level ?? .normal) != .floating
        applyKeepOnTop(nowPinned)
    }

    // MARK: - Toolbar

    private func installToolbar() {
        let toolbar = NSToolbar(identifier: NSToolbar.Identifier("PivoxAIChatToolbar"))
        toolbar.delegate = self
        toolbar.displayMode = NSToolbar.DisplayMode.iconOnly
        toolbar.allowsUserCustomization = false
        toolbar.autosavesConfiguration = false
        window?.toolbar = toolbar
    }

    nonisolated static let keepOnTopID = NSToolbarItem.Identifier("PivoxAIChat.keepOnTop")
    nonisolated static let dockID = NSToolbarItem.Identifier("PivoxAIChat.dock")

    func toolbarAllowedItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        [.flexibleSpace, Self.keepOnTopID, Self.dockID]
    }

    func toolbarDefaultItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        [.flexibleSpace, Self.keepOnTopID, Self.dockID]
    }

    func toolbar(
        _ toolbar: NSToolbar,
        itemForItemIdentifier itemIdentifier: NSToolbarItem.Identifier,
        willBeInsertedIntoToolbar flag: Bool
    ) -> NSToolbarItem? {
        switch itemIdentifier {
        case Self.keepOnTopID:
            let item = NSToolbarItem(itemIdentifier: itemIdentifier)
            item.label = "Keep on Top"
            item.paletteLabel = "Keep on Top"
            item.toolTip = "Keep this window above other windows"
            item.target = self
            item.action = #selector(toggleKeepOnTop(_:))
            item.isBordered = true
            keepOnTopItem = item
            applyKeepOnTop(UserDefaults.standard.bool(forKey: Self.keepOnTopKey))
            return item

        case Self.dockID:
            let item = NSToolbarItem(itemIdentifier: itemIdentifier)
            item.label = "Dock"
            item.paletteLabel = "Dock"
            item.toolTip = "Dock the chat back into the main window"
            item.image = NSImage(
                systemSymbolName: "arrow.down.left.square",
                accessibilityDescription: "Dock the chat back into the main window")
            item.target = self
            item.action = #selector(dockBack(_:))
            item.isBordered = true
            return item

        default:
            return nil
        }
    }

    @objc private func dockBack(_ sender: Any?) {
        AppDelegate.shared?.dockAIChat()
    }

    private func recenterIfOffScreen() {
        guard let window else { return }
        let frame = window.frame
        let minVisible: CGFloat = 120
        let anyScreenShowsEnough = NSScreen.screens.contains { screen in
            let intersection = screen.visibleFrame.intersection(frame)
            return intersection.width >= minVisible
                && intersection.height >= minVisible
        }
        if !anyScreenShowsEnough {
            window.center()
        }
    }
}
