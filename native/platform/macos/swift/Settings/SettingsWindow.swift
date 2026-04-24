import AppKit
import SwiftUI

/// Non-modal settings window, opened via the app menu (⌘,) or the
/// sidebar profile button.
///
/// ## Tab strip
/// The tab strip at the top is rendered by an `NSToolbar` in
/// `.preference` style — icon-over-label items, centered,
/// with the selected item highlighted in the accent color. This
/// is how Safari, Mail, Xcode, and System Settings render their
/// Settings tabs. SwiftUI's `TabView` doesn't produce this look
/// outside a `Settings { }` scene (labels only, no icons), so we
/// own the toolbar ourselves.
///
/// ## Sizing behavior
/// Width is fixed (set by the tabs themselves). Height adapts per
/// tab via our own `ResizingHostingController` which reports
/// `view.fittingSize` changes on every layout pass; we animate
/// the window to match via `NSAnimationContext` +
/// `window.animator().setFrame`.
@MainActor
final class SettingsWindowController: NSWindowController, NSToolbarDelegate {
    /// @Observable selection holder — mutating `tab` imperatively
    /// in `show(tab:)` or in the toolbar click handler propagates
    /// straight through the binding we pass to `SettingsView`.
    private let selection = SettingsSelection()

    private let hostingController: ResizingHostingController<SettingsView>

    init() {
        let hosting = ResizingHostingController(
            rootView: SettingsView(selection: selection))
        self.hostingController = hosting

        let initial = NSRect(x: 0, y: 0, width: 640, height: 420)
        let window = NSWindow(
            contentRect: initial,
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = hosting.view
        // Settings-style toolbar (icon-over-label, centered). Same
        // treatment System Settings / Safari / Xcode use.
        window.toolbarStyle = .preference
        // Title reflects the current tab, like Safari does.
        window.title = selection.tab.label

        let autosaveName = NSWindow.FrameAutosaveName("PivoxSettings")
        let restored = window.setFrameUsingName(autosaveName)
        window.setFrameAutosaveName(autosaveName)
        if !restored {
            window.center()
        }

        super.init(window: window)

        hosting.onPreferredSizeChange = { [weak self] size in
            self?.resizeWindow(to: size)
        }

        installToolbar()
    }

    required init?(coder: NSCoder) { fatalError("not implemented") }

    /// Bring the window forward on the requested tab.
    func show(tab: SettingsView.Tab) {
        applyTab(tab)
        recenterIfOffScreen()
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    /// Single path that writes the selection — updates the
    /// observable model, the toolbar selection, and the window
    /// title. Keeps the three in sync regardless of whether the
    /// change came from `show(tab:)` or a toolbar click.
    private func applyTab(_ tab: SettingsView.Tab) {
        selection.tab = tab
        window?.toolbar?.selectedItemIdentifier = tab.toolbarIdentifier
        window?.title = tab.label
    }

    // MARK: - Toolbar

    private func installToolbar() {
        let toolbar = NSToolbar(identifier: NSToolbar.Identifier("PivoxSettingsToolbar"))
        toolbar.delegate = self
        toolbar.displayMode = NSToolbar.DisplayMode.iconAndLabel
        toolbar.allowsUserCustomization = false
        toolbar.autosavesConfiguration = false
        window?.toolbar = toolbar
        toolbar.selectedItemIdentifier = selection.tab.toolbarIdentifier
    }

    @objc private func toolbarItemClicked(_ sender: NSToolbarItem) {
        guard let tab = SettingsView.Tab(toolbarIdentifier: sender.itemIdentifier) else { return }
        applyTab(tab)
    }

    func toolbarAllowedItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        SettingsView.Tab.allCases.map(\.toolbarIdentifier)
    }

    func toolbarDefaultItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        SettingsView.Tab.allCases.map(\.toolbarIdentifier)
    }

    func toolbarSelectableItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        SettingsView.Tab.allCases.map(\.toolbarIdentifier)
    }

    func toolbar(
        _ toolbar: NSToolbar,
        itemForItemIdentifier itemIdentifier: NSToolbarItem.Identifier,
        willBeInsertedIntoToolbar flag: Bool
    ) -> NSToolbarItem? {
        guard let tab = SettingsView.Tab(toolbarIdentifier: itemIdentifier) else { return nil }
        let item = NSToolbarItem(itemIdentifier: itemIdentifier)
        item.label = tab.label
        item.paletteLabel = tab.label
        item.image = NSImage(
            systemSymbolName: tab.iconSymbol,
            accessibilityDescription: tab.label)
        item.target = self
        item.action = #selector(toolbarItemClicked(_:))
        item.isBordered = false
        return item
    }

    // MARK: - Geometry

    /// Verify the saved window frame still has meaningful overlap
    /// with a connected screen's visible area. If not, re-center
    /// on the main screen so the user doesn't lose the window.
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

    private func resizeWindow(to contentSize: CGSize) {
        guard let window else { return }
        let oldFrame = window.frame
        let newFrameRect = window.frameRect(
            forContentRect: NSRect(origin: .zero, size: contentSize))
        let newFrame = NSRect(
            x: oldFrame.origin.x,
            y: oldFrame.maxY - newFrameRect.size.height,
            width: newFrameRect.size.width,
            height: newFrameRect.size.height)
        guard newFrame != oldFrame else { return }

        NSAnimationContext.runAnimationGroup { ctx in
            ctx.duration = 0.22
            ctx.allowsImplicitAnimation = true
            window.animator().setFrame(newFrame, display: true)
        }
    }
}

/// @Observable tab-selection holder. Living outside the SwiftUI
/// view lets the window controller mutate the selection
/// imperatively (`show(tab:)` or a toolbar click) while SwiftUI
/// still re-renders automatically when `tab` changes.
@Observable
@MainActor
final class SettingsSelection {
    var tab: SettingsView.Tab = .general
}

/// NSHostingController subclass that reports SwiftUI's intrinsic
/// content size via `view.fittingSize` on every layout pass. The
/// window controller uses this to drive its own animated resize.
final class ResizingHostingController<Content: View>: NSHostingController<Content> {
    var onPreferredSizeChange: ((CGSize) -> Void)?
    private var lastReportedSize: CGSize = .zero

    override func viewDidLayout() {
        super.viewDidLayout()
        let size = view.fittingSize
        guard size != .zero, size != lastReportedSize else { return }
        lastReportedSize = size
        onPreferredSizeChange?(size)
    }
}

// MARK: - Tab ↔︎ toolbar identifier bridge

extension SettingsView.Tab {
    var toolbarIdentifier: NSToolbarItem.Identifier {
        NSToolbarItem.Identifier(rawValue: "PivoxSettings.\(rawValue)")
    }

    init?(toolbarIdentifier id: NSToolbarItem.Identifier) {
        let prefix = "PivoxSettings."
        guard id.rawValue.hasPrefix(prefix) else { return nil }
        let raw = String(id.rawValue.dropFirst(prefix.count))
        guard let tab = SettingsView.Tab(rawValue: raw) else { return nil }
        self = tab
    }
}
