import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate {
  /// Convenience accessor for the live AppDelegate instance. Set up
  /// in `main.swift` as `NSApp.delegate`; this casts back for code
  /// in SwiftUI views that needs to reach into AppKit-hosted
  /// windows (like the Settings window).
  static var shared: AppDelegate? { NSApp.delegate as? AppDelegate }

  var window: NSWindow?
  private let appState = AppStateBridge.shared()

  /// Lazily-initialized settings window controller. The first call
  /// to `showSettings(tab:)` creates it; every subsequent call
  /// reuses it and just brings the window forward — same pattern
  /// Apple uses for System Settings, Xcode Preferences, etc.
  private var settingsWindowController: SettingsWindowController?

  /// Key under which we persist the last-used Settings tab so ⌘,
  /// returns the user to where they were last.
  private static let lastSettingsTabKey = "settings.last_tab"

  // Delegated auth (AUTHN-07): each `pivox://auth/delegate/signin?session=…`
  // deep link gets its own coordinator, its own NSWindow, and its own named
  // Firebase app. Multiple concurrent flows are supported — the coordinator
  // is keyed by session code on this side.
  private var delegatedFlows: [String: DelegatedFlow] = [:]

  // True when the cold-launch URL was a delegated auth link. Set before
  // didFinishLaunching by the NSAppleEventManager handler, so the main
  // window creation path can skip the main window entirely for signin /
  // signout actions — the user came from a plugin and has no reason to see
  // the Pivox app proper. After the delegated flow completes, this flag
  // also drives the terminate-vs-stay decision in finishDelegatedFlow.
  private var wasLaunchedForDelegatedAuth = false

  // URL captured before Firebase was configured. Drained in
  // applicationDidFinishLaunching after AuthService.configure() runs.
  private var pendingColdLaunchURL: URL?

  // Flips true at the end of applicationDidFinishLaunching. URLs arriving
  // before this point are "cold launch"; after, they are "running app".
  private var didFinishLaunch = false

  private struct DelegatedFlow {
    let coordinator: DelegatedAuthCoordinator
    let window: NSWindow
  }

  // MARK: - Launch lifecycle

  func applicationWillFinishLaunching(_ notification: Notification) {
    // Shorter tooltip delay than the macOS default (1000ms). AppKit's
    // NSToolTipManager reads this default at app startup; register
    // before any UI is built. 400ms matches the Gemini-era feel —
    // quick enough to feel responsive, long enough to not pop on
    // casual cursor sweeps.
    UserDefaults.standard.register(defaults: ["NSInitialToolTipDelay": 400])

    // Register the classic kAEGetURL handler so we can catch cold-launch
    // URLs *before* applicationDidFinishLaunching runs. NSApplicationDelegate's
    // `application(_:open:)` fires after the main window is already on
    // screen, which is too late to decide whether to create that window.
    NSAppleEventManager.shared().setEventHandler(
      self,
      andSelector: #selector(handleGetURLEvent(_:withReplyEvent:)),
      forEventClass: AEEventClass(kInternetEventClass),
      andEventID: AEEventID(kAEGetURL)
    )

    #if UITEST
      // UI-test-only hook: simulate a cold-launch URL arrival. XCUITest can't
      // send AppleEvents, so tests set PIVOX_TEST_DEEP_LINK on
      // launchEnvironment and we pump it through the same capture path the
      // real handler uses. Gated on #if UITEST so production binaries never
      // read the variable.
      if let raw = ProcessInfo.processInfo.environment["PIVOX_TEST_DEEP_LINK"],
        let url = URL(string: raw)
      {
        captureIncomingURL(url)
      }
    #endif
  }

  func applicationDidFinishLaunching(_ notification: Notification) {
    // Initialize Firebase before any UI.
    AuthService.shared.configure()

    // Install the shared gRPC auth token provider. Every RPC client
    // (ChatClient and future services) reads tokens through this —
    // ChatClient no longer takes an authToken parameter.
    PivoxAuthBridge.registerTokenProvider()

    // If a delegated signin/signout link was captured during will-launch,
    // skip creating the main window entirely — there is no Pivox UI the user
    // wants to see. Profile is an explicit "open the app" request so it keeps
    // the main window.
    let coldLaunchAction: DelegatedAuthDeepLink.Action? = pendingColdLaunchURL.flatMap {
      DelegatedAuthDeepLink.parse($0)?.action
    }
    let skipMainWindow = coldLaunchAction == .signin || coldLaunchAction == .signout

    if !skipMainWindow {
      createMainWindow()
    }
    setupMainMenu()
    NSApp.activate(ignoringOtherApps: true)

    didFinishLaunch = true

    // Dispatch any captured cold-launch URL on the next run-loop tick —
    // NOT synchronously here. SwiftUI hasn't finished wiring up .onReceive
    // subscribers yet, so a notification posted right now (e.g. the
    // profile-navigation event) would be dropped. Deferring one tick
    // also lets NSApp.terminate() unwind cleanly for cold-launch signout,
    // which otherwise runs inside -applicationDidFinishLaunching: before
    // the main run loop is fully spinning.
    if let pending = pendingColdLaunchURL {
      pendingColdLaunchURL = nil
      DispatchQueue.main.async { [weak self] in
        self?.dispatchIncomingURL(pending)
      }
    }
  }

  private func createMainWindow() {
    let contentView = ContentView()
      .frame(minWidth: 1024, minHeight: 768)

    // Restore saved window state or use defaults.
    let width = appState.hasWindowState() ? appState.windowWidth() : 1280
    let height = appState.hasWindowState() ? appState.windowHeight() : 800

    let win = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: Int(width), height: Int(height)),
      styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
      backing: .buffered,
      defer: false
    )
    win.title = "Pivox"
    // Minimum content size = sum of each column's minimum. Prevents
    // the user from resizing the window small enough to squish the
    // sidebar below its own min when chat is open:
    //   sidebar (220) + main detail (400) + chat panel (320) = 940
    //   vertical needs ~1 full chat message visible: 500
    // Without this, SwiftUI's column-width negotiation crushes
    // whichever column is weakest (historically the sidebar).
    win.contentMinSize = NSSize(width: 940, height: 500)
    // Let the window itself be translucent so NavigationSplitView's
    // built-in sidebar material can blur the desktop wallpaper
    // behind it (Music/Finder bleed effect). On macOS 26 this uses
    // Liquid Glass rendering and respects the user's Clear/Tinted
    // preference automatically; on older macOS it falls back to the
    // classic NSVisualEffectView translucent sidebar. Detail /
    // chat-panel views supply their own opaque-ish backgrounds, so
    // making the window itself clear only affects the sidebar column
    // (which is the one we want glass on).
    win.isOpaque = false
    win.backgroundColor = .clear
    win.contentView = NSHostingView(rootView: contentView)

    if appState.hasWindowState() {
      let x = appState.windowX()
      let y = appState.windowY()
      win.setFrameOrigin(NSPoint(x: Int(x), y: Int(y)))
    } else {
      win.center()
    }

    win.makeKeyAndOrderFront(nil)

    // Observe window move/resize to persist state.
    NotificationCenter.default.addObserver(
      self, selector: #selector(windowDidResize),
      name: NSWindow.didResizeNotification, object: win)
    NotificationCenter.default.addObserver(
      self, selector: #selector(windowDidMove),
      name: NSWindow.didMoveNotification, object: win)

    self.window = win
  }

  func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    return true
  }

  // MARK: - Deep link handling

  /// kAEGetURL handler — invoked by AppKit when the system delivers a
  /// `pivox://` URL to this app, either at cold launch (before
  /// applicationDidFinishLaunching) or while running.
  @objc
  private func handleGetURLEvent(
    _ event: NSAppleEventDescriptor,
    withReplyEvent reply: NSAppleEventDescriptor
  ) {
    guard
      let urlString = event.paramDescriptor(forKeyword: AEKeyword(keyDirectObject))?.stringValue,
      let url = URL(string: urlString)
    else { return }
    captureIncomingURL(url)
  }

  /// Route a `pivox://` URL. If the app is still launching, the URL is
  /// stashed for didFinishLaunching to drain; otherwise it dispatches
  /// immediately. Only delegated-auth URLs are tracked here — other schemes
  /// (OAuth redirects, etc.) flow through the Firebase SDK separately.
  private func captureIncomingURL(_ url: URL) {
    guard DelegatedAuthDeepLink.parse(url) != nil else { return }

    if didFinishLaunch {
      Task { @MainActor in self.dispatchIncomingURL(url) }
    } else {
      wasLaunchedForDelegatedAuth = true
      pendingColdLaunchURL = url
    }
  }

  @MainActor
  private func dispatchIncomingURL(_ url: URL) {
    guard let deepLink = DelegatedAuthDeepLink.parse(url) else { return }
    handleDelegatedAuth(deepLink)
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
      // Defer the actual terminate by one run-loop cycle + a small margin
      // so that (a) any just-posted NSApplicationWillTerminate observers
      // run cleanly and (b) XCUITest's accessibility bridge — which
      // attaches asynchronously after applicationDidFinishLaunching —
      // gets a chance to connect before the process exits. Without the
      // margin, XCUITest reports "application has not loaded accessibility"
      // and the UI test that drives this path times out.
      if wasLaunchedForDelegatedAuth || !wasSignedIn {
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
          NSApp.terminate(nil)
        }
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
    guard let frame = window?.frame else { return }
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
    let settingsItem = NSMenuItem(
      title: "Settings…",
      action: #selector(openSettingsAction(_:)),
      keyEquivalent: ",")
    settingsItem.target = self
    appMenu.addItem(settingsItem)
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

  // MARK: - Settings window

  /// Open the Settings window on a specific tab. Creates the
  /// window controller on first call and reuses it afterwards;
  /// the controller's `onTabChanged` callback (wired here)
  /// persists the tab on every change — including in-window
  /// toolbar clicks — so the next ⌘, returns to the same place.
  @MainActor
  func showSettings(tab: SettingsView.Tab) {
    let controller = settingsWindowController ?? {
      let c = SettingsWindowController()
      c.onTabChanged = { tab in
        UserDefaults.standard.set(tab.rawValue, forKey: Self.lastSettingsTabKey)
      }
      settingsWindowController = c
      return c
    }()
    controller.show(tab: tab)
  }

  /// ⌘, target. Opens Settings on whatever tab the user was on
  /// last (General if no choice has been persisted yet). The
  /// sidebar profile menu's "Settings…" item intentionally
  /// omits the ⌘, key equivalent because it always lands on
  /// Account, which doesn't match what ⌘, does.
  @objc private func openSettingsAction(_ sender: Any?) {
    let raw = UserDefaults.standard.string(forKey: Self.lastSettingsTabKey) ?? ""
    let tab = SettingsView.Tab(rawValue: raw) ?? .general
    Task { @MainActor in self.showSettings(tab: tab) }
  }
}
