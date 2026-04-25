import AppKit
import SwiftUI

/// Floating AI Chat panel. Hosts `AIChatContainerView` in an
/// `NSPanel` (not a regular `NSWindow`) so the chat behaves as a
/// secondary tool — Apple HIG's idiom for focused, auxiliary
/// surfaces — rather than a peer document window. Concretely that
/// means:
///
///   - `.utilityWindow` style mask → smaller title bar that visually
///     distinguishes the panel from the primary window.
///   - `isFloatingPanel = true` → panel floats above the main
///     window automatically (no user-toggleable "keep on top" knob,
///     which is a Windows-ism HIG doesn't bless).
///   - No `.miniaturizable` → panels aren't minimized (Mac
///     convention; the close button is the dismissal affordance).
///   - We deliberately don't set `becomesKeyOnlyIfNeeded` — that
///     flag only matters in combination with `.nonactivatingPanel`
///     (which we don't use; the chat needs full keyboard focus).
///     Without `.nonactivatingPanel`, a regular click promotes the
///     panel to key window automatically, which is what we want.
///
/// No `NSToolbar`. The "Show in Main Window" affordance is rendered
/// inside `AIChatContainerView`'s header as an `IconButton` so it
/// matches the inline-mode "Open in Window" button visually — one
/// SwiftUI component, one set of sizing/styling tokens. NSToolbar
/// in `.utilityWindow` panels renders small by AppKit design (HIG-
/// correct for tool palettes), but mixing those small toolbar icons
/// with our chunky `IconButton` glyphs in the same surface looked
/// inconsistent. Putting the affordance in the SwiftUI header keeps
/// both modes visually identical.
///
/// The panel lives across open/close cycles
/// (`isReleasedWhenClosed = false`) so the chat's SwiftUI state —
/// in-progress conversation, message draft, ChatClient connection —
/// survives a red-traffic-light close. Reopening brings the same
/// surface back. The shared `AIChatService` keeps the underlying
/// view model + gRPC channel alive across inline ↔ panel mode swaps.
@MainActor
final class AIChatWindowController: NSWindowController, NSWindowDelegate {
    /// Fired when the user closes the panel (red traffic light or
    /// ⌘W). The owner uses this to keep visibility state in sync.
    var onWindowClosed: (() -> Void)?

    private let frameAutosaveName = NSWindow.FrameAutosaveName("PivoxAIChat")

    init() {
        let hosting = NSHostingController(rootView: AIChatContainerView())
        let initial = NSRect(x: 0, y: 0, width: 400, height: 700)
        let panel = NSPanel(
            contentRect: initial,
            styleMask: [.titled, .closable, .resizable, .utilityWindow],
            backing: .buffered,
            defer: false)
        panel.title = "Pivox AI"
        panel.isReleasedWhenClosed = false
        panel.contentView = hosting.view
        // Explicit opaque backdrop. We removed the SwiftUI-side
        // `.background(.background)` from `AIChatContainerView` so
        // the inline float-mode wrapper can paint `.thinMaterial`
        // instead — but that left the detached panel relying on
        // whatever default the `.utilityWindow` style mask
        // provides. On macOS versions where utility panels enable
        // vibrancy on the content area, message bubbles + composer
        // would sit on a tinted/vibrant background. Pinning the
        // panel's `backgroundColor` and `isOpaque = true` defeats
        // that and gives the detached chat a stable solid backing.
        panel.backgroundColor = .windowBackgroundColor
        panel.isOpaque = true
        // Floating-by-default behavior for inspector-style panels.
        // Replaces the previous user-toggled "keep on top" knob; HIG
        // expects panels to float without per-window user controls.
        panel.isFloatingPanel = true
        // Stay visible across app deactivation so users on
        // multi-monitor setups can keep the chat in view while
        // working in another app.
        panel.hidesOnDeactivate = false
        // Width bounds for the floating panel.
        //
        // Min 380: leaves room for the composer's full keyboard
        // hint (`↩ Send · ⌥↩ New line`) at its callout font size
        // without truncating to "↩ Sen…", plus the leading
        // attachment button and trailing send button. Anything
        // narrower starts compressing primary controls.
        //
        // Max 900: comfortable upper bound for chat-bubble line
        // lengths. Beyond that the bubbles stretch into ergonomic
        // dead-zone (>~80 chars per line on body text) and the
        // panel starts feeling like a primary surface rather than
        // an auxiliary tool. Wider than the inline-mode max (640)
        // because the popped-out window has its own breathing room
        // and users on big monitors reasonably want more.
        //
        // We set BOTH the content-size and the window-level
        // size constraints. `contentMinSize`/`contentMaxSize`
        // alone aren't always honored on `.utilityWindow` panels —
        // AppKit doesn't always propagate them to the live-resize
        // handlers. The window-level `minSize`/`maxSize` close
        // that gap.
        //
        // Height floor 480; otherwise grows to fit the user's
        // display (cap at a very large value rather than infinity
        // so the resize cursor has a meaningful upper bound).
        let minContent = NSSize(width: 380, height: 480)
        let maxContent = NSSize(width: 900, height: 30_000)
        panel.contentMinSize = minContent
        panel.contentMaxSize = maxContent
        // Add the title bar height when projecting content size to
        // window size. `.utilityWindow` title bar is ~22pt; use
        // `frameRect(forContentRect:)` so AppKit picks the right
        // value across macOS versions.
        let probeContentRect = NSRect(origin: .zero, size: minContent)
        let probeFrame = panel.frameRect(forContentRect: probeContentRect)
        let titleBarHeight = probeFrame.height - minContent.height
        panel.minSize = NSSize(
            width: minContent.width,
            height: minContent.height + titleBarHeight)
        panel.maxSize = NSSize(
            width: maxContent.width,
            height: maxContent.height + titleBarHeight)

        // Persist + restore frame across launches AND across
        // popout/embed cycles within a single session. Order
        // matters: register the autosave name first so AppKit
        // hooks frame-change saves before we read, then explicitly
        // restore. We also save on close (see `windowWillClose`)
        // because AppKit's automatic save-on-close races with
        // window teardown when `dockAIChat` triggers
        // `performClose` — without the explicit save, dragging the
        // panel to a corner and embedding back loses the position.
        panel.setFrameAutosaveName(frameAutosaveName)
        let restored = panel.setFrameUsingName(frameAutosaveName)
        if !restored { panel.center() }

        super.init(window: panel)
        panel.delegate = self
    }

    required init?(coder: NSCoder) { fatalError("not implemented") }

    /// Bring the panel forward, re-centering if a stale persisted
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
        // Explicit frame save. AppKit's autosave queue can drop
        // pending writes when the controller releases the window
        // reference immediately after `performClose` (which
        // `dockAIChat` does). Saving synchronously here guarantees
        // the user's last position survives the embed-back round
        // trip, regardless of whether the autosave queue had
        // flushed yet.
        window?.saveFrame(usingName: frameAutosaveName)
        onWindowClosed?()
    }

    func windowDidEndLiveResize(_ notification: Notification) {
        // Sticky width: persist the user's resize as soon as the
        // drag ends. AppKit's autosave fires on bounds changes but
        // not always synchronously — saving explicitly here ensures
        // the next popout restores exactly where the user left off,
        // even on app crash mid-session.
        window?.saveFrame(usingName: frameAutosaveName)
    }

    func windowDidMove(_ notification: Notification) {
        // Same rationale as `windowDidEndLiveResize` — make the
        // last-known position deterministic against any autosave
        // timing nuances.
        window?.saveFrame(usingName: frameAutosaveName)
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
