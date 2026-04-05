import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    private let appState = AppStateBridge.shared()!

    func applicationDidFinishLaunching(_ notification: Notification) {
        let contentView = ContentView()
            .frame(minWidth: 1024, minHeight: 768)

        // Restore saved window state or use defaults.
        let width = appState.hasWindowState() ? appState.windowWidth() : 1280
        let height = appState.hasWindowState() ? appState.windowHeight() : 800

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: Int(width), height: Int(height)),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "Pivox"
        window.contentView = NSHostingView(rootView: contentView)

        if appState.hasWindowState() {
            let x = appState.windowX()
            let y = appState.windowY()
            window.setFrameOrigin(NSPoint(x: Int(x), y: Int(y)))
        } else {
            window.center()
        }

        window.makeKeyAndOrderFront(nil)

        // Observe window move/resize to persist state.
        NotificationCenter.default.addObserver(
            self, selector: #selector(windowDidResize),
            name: NSWindow.didResizeNotification, object: window)
        NotificationCenter.default.addObserver(
            self, selector: #selector(windowDidMove),
            name: NSWindow.didMoveNotification, object: window)

        setupMainMenu()

        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return true
    }

    @objc private func windowDidResize(_ notification: Notification) {
        saveWindowState()
    }

    @objc private func windowDidMove(_ notification: Notification) {
        saveWindowState()
    }

    private func saveWindowState() {
        let frame = window.frame
        appState.saveWindowX(Int32(frame.origin.x),
                            y: Int32(frame.origin.y),
                            width: Int32(frame.size.width),
                            height: Int32(frame.size.height))
    }

    private func setupMainMenu() {
        let mainMenu = NSMenu()

        // App menu
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About Pivox", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(NSMenuItem.separator())
        appMenu.addItem(withTitle: "Quit Pivox", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        let appMenuItem = NSMenuItem()
        appMenuItem.submenu = appMenu
        mainMenu.addItem(appMenuItem)

        // File menu
        let fileMenu = NSMenu(title: "File")
        fileMenu.addItem(withTitle: "New Show", action: nil, keyEquivalent: "n")
        fileMenu.addItem(withTitle: "Open Show…", action: nil, keyEquivalent: "o")
        fileMenu.addItem(NSMenuItem.separator())
        fileMenu.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        let fileMenuItem = NSMenuItem()
        fileMenuItem.submenu = fileMenu
        mainMenu.addItem(fileMenuItem)

        // Edit menu
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: #selector(UndoManager.undo), keyEquivalent: "z")
        editMenu.addItem(withTitle: "Redo", action: #selector(UndoManager.redo), keyEquivalent: "Z")
        editMenu.addItem(NSMenuItem.separator())
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        let editMenuItem = NSMenuItem()
        editMenuItem.submenu = editMenu
        mainMenu.addItem(editMenuItem)

        // View menu
        let viewMenu = NSMenu(title: "View")
        viewMenu.addItem(withTitle: "Toggle Sidebar", action: #selector(NSSplitViewController.toggleSidebar(_:)), keyEquivalent: "s")
        let viewMenuItem = NSMenuItem()
        viewMenuItem.submenu = viewMenu
        mainMenu.addItem(viewMenuItem)

        // Window menu
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        let windowMenuItem = NSMenuItem()
        windowMenuItem.submenu = windowMenu
        mainMenu.addItem(windowMenuItem)

        // Help menu
        let helpMenu = NSMenu(title: "Help")
        helpMenu.addItem(withTitle: "Pivox Help", action: nil, keyEquivalent: "?")
        let helpMenuItem = NSMenuItem()
        helpMenuItem.submenu = helpMenu
        mainMenu.addItem(helpMenuItem)

        NSApp.mainMenu = mainMenu
    }
}
