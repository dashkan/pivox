import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate {
  var window: NSWindow!
  private let appState = AppStateBridge.shared()

  // Delegated auth (AUTHN-07): each `pivox://auth/delegate/signin?session=…`
  // deep link gets its own coordinator, its own NSWindow, and its own named
  // Firebase app. Multiple concurrent flows are supported — the coordinator
  // is keyed by session code on this side.
  private var delegatedFlows: [String: DelegatedFlow] = [:]
  // True when the app was launched *because of* a signin deep link and has
  // no other reason to stay open. Used to terminate after the flow finishes.
  private var wasLaunchedForDelegatedAuth = false

  private struct DelegatedFlow {
    let coordinator: DelegatedAuthCoordinator
    let window: NSWindow
  }

  func applicationDidFinishLaunching(_ notification: Notification) {
    // Initialize Firebase before any UI.
    AuthService.shared.configure()

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

    #if UITEST
      // UI-test-only hook: synthesise a delegated auth deep link at launch.
      // XCUITest can't directly drive application(_:open:) from outside the
      // app process, so tests set PIVOX_TEST_DEEP_LINK on launchEnvironment
      // and we pump it through the real handler here. Gated on #if UITEST
      // so production binaries never read the variable.
      if let raw = ProcessInfo.processInfo.environment["PIVOX_TEST_DEEP_LINK"],
        let url = URL(string: raw)
      {
        DispatchQueue.main.async { [weak self] in
          self?.application(NSApp, open: [url])
        }
      }
    #endif
  }

  func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    return true
  }

  // MARK: - Deep link handling

  /// Called by AppKit when the system delivers `pivox://` URLs. Routes
  /// delegated auth links into the coordinator, leaves anything else for
  /// default handling (e.g. OAuth redirects flow through the Firebase SDK).
  @MainActor
  func application(_ application: NSApplication, open urls: [URL]) {
    for url in urls {
      if let deepLink = DelegatedAuthDeepLink.parse(url) {
        handleDelegatedAuth(deepLink)
      }
      // Non-delegate URLs (OAuth callbacks, etc.) are handled elsewhere.
    }
  }

  @MainActor
  private func handleDelegatedAuth(_ deepLink: DelegatedAuthDeepLink) {
    switch deepLink.action {
    case .signin:
      guard let code = deepLink.sessionCode else { return }
      beginDelegatedSignin(sessionCode: code)
    case .profile:
      DelegatedAuthCoordinator.handleProfile()
      // If the app was hidden, surface the main window so the user sees the
      // navigation land on profile.
      NSApp.activate(ignoringOtherApps: true)
      window?.makeKeyAndOrderFront(nil)
    case .signout:
      let wasSignedIn = DelegatedAuthCoordinator.handleSignout()
      // Match Windows: if we were launched just for the signout link, exit.
      if wasLaunchedForDelegatedAuth || !wasSignedIn {
        NSApp.terminate(nil)
      }
    }
  }

  @MainActor
  private func beginDelegatedSignin(sessionCode: String) {
    // Ignore duplicate deep links for a session already in flight.
    if delegatedFlows[sessionCode] != nil { return }

    let coordinator = DelegatedAuthCoordinator()

    let delegatedAuth: AuthService
    do {
      delegatedAuth = try coordinator.beginSignin(sessionCode: sessionCode)
    } catch {
      NSLog("[PivoxApp] Delegated signin init failed: \(error.localizedDescription)")
      return
    }

    let flowWindow = makeDelegatedAuthWindow(authService: delegatedAuth)
    flowWindow.makeKeyAndOrderFront(nil)
    NSApp.activate(ignoringOtherApps: true)

    coordinator.onFinished = { [weak self] result in
      DispatchQueue.main.async {
        self?.finishDelegatedFlow(sessionCode: sessionCode, result: result)
      }
    }

    delegatedFlows[sessionCode] = DelegatedFlow(coordinator: coordinator, window: flowWindow)
  }

  /// Create the NSWindow that hosts the delegated sign-in UI. Reuses the
  /// normal LoginView; only the auth service is swapped for the isolated
  /// named-Firebase instance produced by the coordinator.
  @MainActor
  private func makeDelegatedAuthWindow(authService: AuthService) -> NSWindow {
    let content = DelegatedAuthWindowView(auth: authService)
    let win = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 480, height: 560),
      styleMask: [.titled, .closable, .miniaturizable],
      backing: .buffered,
      defer: false
    )
    win.title = "Sign in to Pivox"
    win.contentView = NSHostingView(rootView: content)
    win.center()
    win.isReleasedWhenClosed = false
    return win
  }

  @MainActor
  private func finishDelegatedFlow(sessionCode: String, result: Result<Void, Error>) {
    guard let flow = delegatedFlows.removeValue(forKey: sessionCode) else { return }
    if case .failure(let error) = result {
      NSLog("[PivoxApp] Delegated signin failed: \(error.localizedDescription)")
    }
    flow.window.orderOut(nil)
    flow.window.close()

    // If the app was launched purely for this flow and the main window isn't
    // visible, terminate — matches the Windows behavior.
    if delegatedFlows.isEmpty && wasLaunchedForDelegatedAuth {
      let mainWindowVisible = window?.isVisible ?? false
      if !mainWindowVisible {
        NSApp.terminate(nil)
      }
    }
  }

  @objc private func windowDidResize(_ notification: Notification) {
    saveWindowState()
  }

  @objc private func windowDidMove(_ notification: Notification) {
    saveWindowState()
  }

  private func saveWindowState() {
    let frame = window.frame
    appState.saveWindowX(
      Int32(frame.origin.x),
      y: Int32(frame.origin.y),
      width: Int32(frame.size.width),
      height: Int32(frame.size.height))
  }

  // swiftlint:disable:next function_body_length
  private func setupMainMenu() {
    let mainMenu = NSMenu()

    // App menu
    let appMenu = NSMenu()
    appMenu.addItem(
      withTitle: "About Pivox", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)),
      keyEquivalent: "")
    appMenu.addItem(NSMenuItem.separator())
    appMenu.addItem(
      withTitle: "Quit Pivox", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    let appMenuItem = NSMenuItem()
    appMenuItem.submenu = appMenu
    mainMenu.addItem(appMenuItem)

    // File menu
    let fileMenu = NSMenu(title: "File")
    fileMenu.addItem(withTitle: "New Show", action: nil, keyEquivalent: "n")
    fileMenu.addItem(withTitle: "Open Show…", action: nil, keyEquivalent: "o")
    fileMenu.addItem(NSMenuItem.separator())
    fileMenu.addItem(
      withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
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
    editMenu.addItem(
      withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
    let editMenuItem = NSMenuItem()
    editMenuItem.submenu = editMenu
    mainMenu.addItem(editMenuItem)

    // View menu
    let viewMenu = NSMenu(title: "View")
    viewMenu.addItem(
      withTitle: "Toggle Sidebar", action: #selector(NSSplitViewController.toggleSidebar(_:)),
      keyEquivalent: "s")
    let viewMenuItem = NSMenuItem()
    viewMenuItem.submenu = viewMenu
    mainMenu.addItem(viewMenuItem)

    // Window menu
    let windowMenu = NSMenu(title: "Window")
    windowMenu.addItem(
      withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
    windowMenu.addItem(
      withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
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
