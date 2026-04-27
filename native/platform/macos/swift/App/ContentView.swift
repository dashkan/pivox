import AppKit
import SwiftUI

enum AppSection: String, CaseIterable, Identifiable {
  case playoutOperator = "Operator"
  case library = "Library"
  case designer = "Designer"
  case engineering = "Engineering"
  case admin = "Admin"

  var id: String { rawValue }

  var icon: String {
    switch self {
    case .playoutOperator: return "play.rectangle"
    case .library: return "photo.on.rectangle"
    case .designer: return "paintbrush"
    case .engineering: return "wrench.and.screwdriver"
    case .admin: return "gearshape"
    }
  }
}

enum SidebarItem: Hashable {
  case section(AppSection)
}

enum AuthState {
  case loggedOut
  case loggedIn
}

struct ContentView: View {
  @State private var selectedItem: SidebarItem?
  @State private var sidebarVisibility: NavigationSplitViewVisibility = .automatic
  @State private var isImageEditing = false
  @State private var sidebarWidth: CGFloat
  @State private var aiToggleHovered = false
  /// Persisted chat panel width. Shared across float and push
  /// layouts so resizing in one mode is reflected when the user
  /// flips to the other. Loaded from AppStateBridge in init,
  /// updated by `ChatResizeHandle` (float) and the push HSplitView
  /// observer, persisted on drag-end (float) / continuously (push).
  @State private var chatPanelWidth: CGFloat = Self.loadChatPanelWidth()
  private var auth = AuthService.shared
  private var aiChatState = AIChatState.shared
  private var orgs = OrgService.shared
  private let appState = AppStateBridge.shared()

  /// Clamp range for the persisted sidebar width. Matches the
  /// `navigationSplitViewColumnWidth(min:ideal:max:)` bounds.
  private static let sidebarMinWidth: CGFloat = 180
  private static let sidebarMaxWidth: CGFloat = 300
  private static let sidebarDefaultWidth: CGFloat = 220

  /// Inline chat panel width bounds. Same as before the dock /
  /// detach split — inline mode reuses these.
  /// Width bounds for the inline chat panel. Same range in both
  /// push and float layouts — one drag affordance, one persisted
  /// value, consistent across mode switches. Range matches the
  /// pre-refactor HSplitView bounds plus a bit of headroom for
  /// users on bigger displays.
  private static let chatMinWidth: CGFloat = 360
  private static let chatMaxWidth: CGFloat = 560
  private static let chatDefaultWidth: CGFloat = 420
  /// Inset of the floating panel from the window edges. 12pt
  /// matches Apple's typical card-floating-over-content depth.
  private static let chatFloatInset: CGFloat = 12
  /// Storage key for the persisted chat panel width.
  private static let chatPanelWidthKey = "chat_panel_width"

  private static func loadChatPanelWidth() -> CGFloat {
    let raw = AppStateBridge.shared().loadString(forKey: chatPanelWidthKey) ?? ""
    let parsed = Double(raw) ?? Double(chatDefaultWidth)
    return CGFloat(parsed).clamped(to: chatMinWidth...chatMaxWidth)
  }

  init() {
    let state = AppStateBridge.shared()

    let saved = state.loadString(forKey: "selected_section")
    if let saved, let section = AppSection(rawValue: saved) {
      _selectedItem = State(initialValue: .section(section))
    } else {
      _selectedItem = State(initialValue: .section(.playoutOperator))
    }

    // Sidebar width: persist across launches so a wider / narrower
    // sidebar chosen by the user sticks.
    let savedSidebarWidth = state.loadString(forKey: "sidebar_width")
      .flatMap { Double($0) }.map { CGFloat($0) } ?? Self.sidebarDefaultWidth
    _sidebarWidth = State(initialValue:
      min(max(savedSidebarWidth, Self.sidebarMinWidth), Self.sidebarMaxWidth))
  }

  var body: some View {
    Group {
      if auth.isSignedIn {
        signedInRoot
      } else {
        AuthRouter()
      }
    }
    .onChange(of: selectedItem) { _, newValue in
      if case .section(let section) = newValue {
        appState.save(section.rawValue, forKey: "selected_section")
      }
    }
    .onChange(of: auth.isSignedIn) { _, isSignedIn in
      // Restore the AI Chat window only after the user is signed
      // in, since the chat is account-scoped. Persists open/closed
      // across launches via UserDefaults inside AppDelegate.
      if isSignedIn {
        AppDelegate.shared?.restoreAIChatIfNeeded()
        Task { await orgs.bootstrap() }
      }
    }
    .task {
      if auth.isSignedIn {
        AppDelegate.shared?.restoreAIChatIfNeeded()
        await orgs.bootstrap()
      }
    }
    .onReceive(
      NotificationCenter.default.publisher(for: DelegatedAuthCoordinator.openProfileNotification)
    ) { _ in
      AppDelegate.shared?.showSettings(tab: .account)
    }
  }

  /// Prefer the user's custom photo, fall back to any linked provider's
  /// photo (Google/GitHub/etc). When a user signs in via Google and
  /// hasn't set a custom photo, Firebase doesn't always mirror Google's
  /// avatar into `user.photoURL` — we dig it out of `providerData`
  /// ourselves so the bar has a picture instead of a silhouette.
  ///
  /// Reads `auth.profileRevision` to establish an `@Observable`
  /// dependency on profile mutations. Firebase's `User` is mutated
  /// in place on photo/name edits, so without this touchpoint the
  /// view never re-evaluates and the sidebar keeps the stale photo.
  private var effectivePhotoURL: URL? {
    _ = auth.profileRevision
    if let url = auth.currentUser?.photoURL { return url }
    return auth.currentUser?.providerData
      .compactMap(\.photoURL)
      .first
  }

  /// Branch between loading splash, onboarding, and the main app
  /// shell once the user is signed in. The splash and onboarding
  /// reuse the same accent backdrop + glass card aesthetic as the
  /// auth views so the transition from registration → org creation
  /// → app feels like one continuous flow.
  @ViewBuilder
  private var signedInRoot: some View {
    switch orgs.state {
    case .idle, .loading:
      OrgLoadingView()
    case .empty:
      CreateOrgView()
    case .ready:
      mainAppView
    case .error(let message):
      OrgLoadErrorView(message: message) {
        Task { await orgs.reload() }
      }
    }
  }

  private var mainAppView: some View {
    // Chat layout branches on `aiChatState.layoutMode`:
    //
    //   - `.push`: HStack peer of NavigationSplitView with the chat
    //     panel as a fixed-width column. Canvas resizes to make
    //     room. Mac-conservative pattern (Mail/Slack/Cursor/Xcode).
    //   - `.float`: ZStack with the canvas full-width and the chat
    //     panel as a translucent card overlaid on the right. Canvas
    //     keeps its full geometric width; content peeks through the
    //     panel's `.regularMaterial` (Liquid Glass on macOS 26+).
    //
    // The chat panel itself is the same view in both layouts — only
    // the framing wrapper changes.
    Group {
      switch aiChatState.layoutMode {
      case .push:
        pushLayout
      case .float:
        floatLayout
      }
    }
    .toolbar {
      // Toolbar lives at the outer level so `.primaryAction`
      // anchors at the window's right edge regardless of the inner
      // layout choice.
      ToolbarItem(placement: .primaryAction) {
        Button {
          AppDelegate.shared?.toggleAIChat(focusInputOnOpen: false)
        } label: {
          Image(systemName: "sparkles")
            .pivoxIconToolbar()
            .symbolVariant(aiChatState.isVisible ? .fill : .none)
            .aiShimmerSymbol(isActive: aiToggleHovered)
            .padding(6)
            .contentShape(Rectangle())
            .onHover { aiToggleHovered = $0 }
        }
        .buttonStyle(.plain)
        .simultaneousGesture(
          TapGesture().onEnded {
            DispatchQueue.main.async {
              if aiChatState.isVisible {
                NotificationCenter.default.post(name: .aiChatFocusRequested, object: nil)
              }
            }
          }
        )
        .help("Toggle AI Chat (⌘⇧A)")
      }
    }
    .background {
      Button {
        AppDelegate.shared?.toggleAIChat(focusInputOnOpen: true)
      } label: {
        EmptyView()
      }
      .keyboardShortcut("a", modifiers: [.command, .shift])
      .buttonStyle(.plain)
      .frame(width: 0, height: 0)
      .opacity(0)
      .accessibilityHidden(true)
      .focusable(false)
    }
  }

  /// Push layout: chat panel takes a column on the right of an
  /// `HSplitView`, canvas resizes to fit. The HSplitView's drag
  /// handle is the native Mac affordance for column resizing;
  /// the persisted width (`chatPanelWidth`) is fed in as `ideal`
  /// and observed via GeometryReader so user drags persist
  /// without us writing back to `idealWidth` mid-render (which
  /// would feedback-loop with HSplitView's internal sizing).
  ///
  /// The opaque `.windowBackgroundColor` backdrop is set here (not
  /// in `InlineAIChatPanel`) so float layout can paint material
  /// instead. The main window has `isOpaque = false` to enable
  /// wallpaper bleed in the sidebar; without an opaque backdrop on
  /// the chat column, that bleed would leak into the chat too.
  private var pushLayout: some View {
    HSplitView {
      navigationSplitView
        .frame(minWidth: 580, maxWidth: .infinity)
      if aiChatState.isVisible && aiChatState.mode == .docked {
        InlineAIChatPanel()
          .frame(minWidth: Self.chatMinWidth,
                 idealWidth: chatPanelWidth,
                 maxWidth: Self.chatMaxWidth)
          .background(Color(nsColor: .windowBackgroundColor))
          .background(
            GeometryReader { proxy in
              Color.clear.onChange(of: proxy.size.width) { _, newWidth in
                guard newWidth >= Self.chatMinWidth,
                      newWidth <= Self.chatMaxWidth else { return }
                chatPanelWidth = newWidth
                appState.save(String(Double(newWidth)),
                              forKey: Self.chatPanelWidthKey)
              }
            }
          )
          .transition(.move(edge: .trailing).combined(with: .opacity))
      }
    }
    .animation(.easeOut(duration: 0.22), value: aiChatState.isVisible)
  }

  /// Float layout: chat panel renders as a translucent card
  /// overlaid on top of the canvas. Canvas geometry is unchanged
  /// when the panel toggles in/out — no reflow.
  private var floatLayout: some View {
    ZStack(alignment: .topTrailing) {
      navigationSplitView
        .frame(minWidth: 580, maxWidth: .infinity)
      if aiChatState.isVisible && aiChatState.mode == .docked {
        InlineAIChatPanel()
          .frame(width: chatPanelWidth)
          // Liquid Glass background. `.thinMaterial` lets more
          // canvas content bleed through than `.regularMaterial`
          // — visible glass effect on macOS 26+, real
          // translucency on older versions. Bubble content keeps
          // its solid `theme.userBubble` / `theme.assistantBubble`
          // colors (set inside the chat views) so messages remain
          // legible against the bleed-through.
          .background(.thinMaterial)
          .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
          .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
          // AppKit-native resize handle — see ChatResizeHandle for
          // the rationale (SwiftUI drag gestures + `.thinMaterial`
          // panel = visible flicker). Overlay sits AFTER `.clipShape`
          // so the 6pt strip isn't clipped at the rounded corners.
          .overlay(alignment: .leading) {
            ChatResizeHandle(
              width: $chatPanelWidth,
              range: Self.chatMinWidth...Self.chatMaxWidth,
              onEnded: { final in
                appState.save(String(Double(final)),
                              forKey: Self.chatPanelWidthKey)
              }
            )
            .frame(width: 6)
          }
          // Shadow gives the card a "floating above" depth. Cast
          // primarily to the LEFT (negative x) since the panel sits
          // against the window's right edge — shadow on the right
          // would clip outside the window.
          .shadow(color: .black.opacity(0.18), radius: 16, x: -4, y: 4)
          .padding(.top, Self.chatFloatInset)
          .padding(.trailing, Self.chatFloatInset)
          .padding(.bottom, Self.chatFloatInset)
          .transition(.move(edge: .trailing).combined(with: .opacity))
      }
    }
    .animation(.easeOut(duration: 0.22), value: aiChatState.isVisible)
  }

  /// Shared sidebar + detail navigation, used by both layout modes.
  /// Extracted to avoid duplicating the NavigationSplitView body
  /// across `pushLayout` / `floatLayout`.
  private var navigationSplitView: some View {
    NavigationSplitView(columnVisibility: $sidebarVisibility) {
        SidebarNavList(selectedItem: $selectedItem)
          .navigationSplitViewColumnWidth(
            min: Self.sidebarMinWidth,
            ideal: sidebarWidth,
            max: Self.sidebarMaxWidth)
          // Observe actual rendered width and persist user drags
          // across launches. Follows the same "persist only, don't
          // write back to `sidebarWidth` at runtime" pattern as the
          // chat panel — a runtime writeback creates a feedback
          // loop with `ideal:`.
          .background(
            GeometryReader { proxy in
              Color.clear.onChange(of: proxy.size.width) { _, newWidth in
                guard newWidth >= Self.sidebarMinWidth,
                      newWidth <= Self.sidebarMaxWidth else { return }
                appState.save(String(Double(newWidth)),
                              forKey: "sidebar_width")
              }
            }
          )
          // ProfileBar overlays the sidebar at the bottom as a
          // glass bar — sidebar content scrolls PAST it (blurred
          // behind the ProfileBar's material). `SidebarNavList`
          // reserves a matching bottom content inset so the last
          // real row stays reachable. `safeAreaInset` (which would
          // push content up instead of overlaying) doesn't propagate
          // into our AppKit NSTableView anyway, so overlay is the
          // only path to Music/Finder-style bleed.
          .overlay(alignment: .bottom) {
            ProfileBar(
              photoURL: effectivePhotoURL,
              displayName: auth.currentUser?.displayName,
              email: auth.currentUser?.email,
              onSettings: { AppDelegate.shared?.showSettings(tab: .account) },
              onSecurity: { AppDelegate.shared?.showSettings(tab: .security) },
              onSignOut: { auth.signOut() }
            )
          }
      } detail: {
        mainDetail
      }
  }

  private var mainDetail: some View {
    // All section branches render inside the same outer container so
    // HSplitView sees a stable child type across sidebar changes.
    // Previously, Library returned `LibraryPlaceholderView` while
    // other sections returned a `VStack`, and switching to Library
    // caused HSplitView to re-evaluate proposed widths — visible as
    // the AI chat panel resizing. Wrapping everything in the same
    // outer view keeps the proposal stable.
    ZStack {
      switch selectedItem {
      case .section(let section):
        if section == .library {
          LibraryPlaceholderView(
            isEditing: $isImageEditing,
            sidebarVisibility: $sidebarVisibility
          )
        } else {
          VStack {
            Text(section.rawValue)
              .font(.largeTitle)
              .foregroundStyle(.secondary)
            Text("Coming soon")
              .foregroundStyle(.tertiary)
          }
        }
      case nil:
        Text("Select a section")
          .foregroundStyle(.secondary)
      }
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    // Opaque backing so the main detail area doesn't inherit the
    // transparent window and show desktop wallpaper through the
    // whole app. The window is set to `isOpaque = false` at the
    // AppDelegate level to let NavigationSplitView's sidebar
    // material bleed through; detail needs its own fill because
    // it's not a sidebar material view.
    .background(Color(nsColor: .windowBackgroundColor))
  }
}

/// Temporary Library placeholder with image edit test.
struct LibraryPlaceholderView: View {
  @Binding var isEditing: Bool
  @Binding var sidebarVisibility: NavigationSplitViewVisibility
  @State private var selectedImage: NSImage?
  @State private var showEditor = false
  @State private var cropResult: String?
  @State private var didAutoLoad = false

  var body: some View {
    if let image = selectedImage, showEditor {
      // `.preferredColorScheme(.dark)` is scoped to ImageEditView
      // so the editor renders dark without forcing
      // `NSApp.keyWindow.appearance = .darkAqua` on the entire
      // window. Window-level flips re-resolve every appearance-
      // dependent surface (including the chat panel's
      // `.thinMaterial`), causing the chat to re-render mid-drag
      // and the resize handle's gesture state to reset. Scoping
      // dark mode here keeps the rest of the window's appearance
      // unchanged.
      ImageEditView(
        image: image,
        isEditing: $isEditing,
        sidebarVisibility: $sidebarVisibility,
        onDone: { rect in
          cropResult = "Crop: x=\(rect.x) y=\(rect.y) w=\(rect.width) h=\(rect.height)"
          closeEditor()
        },
        onCancel: {
          closeEditor()
        }
      )
      .preferredColorScheme(.dark)
    } else {
      VStack(spacing: 16) {
        Spacer()
        Text("Library")
          .font(.largeTitle)
          .foregroundStyle(.secondary)

        Button("Open Image to Edit") {
          let panel = NSOpenPanel()
          panel.allowedContentTypes = [.image]
          panel.allowsMultipleSelection = false
          if panel.runModal() == .OK, let url = panel.url,
            let image = NSImage(contentsOf: url)
          {
            selectedImage = image
            showEditor = true
          }
        }
        .controlSize(.large)
        .accessibilityIdentifier("library-open-image")

        if let result = cropResult {
          Text(result)
            .font(.caption)
            .foregroundStyle(.secondary)
            .padding(.top, 8)
            .accessibilityIdentifier("library-crop-result")
        }
        Spacer()
      }
      .frame(maxWidth: .infinity, maxHeight: .infinity)
      .onAppear {
        guard !didAutoLoad else { return }
        didAutoLoad = true
        if let path = ProcessInfo.processInfo.environment["TEST_IMAGE_PATH"],
          let image = NSImage(contentsOfFile: path)
        {
          selectedImage = image
          showEditor = true
        }
      }
    }
  }

  private func closeEditor() {
    showEditor = false
    selectedImage = nil
    isEditing = false
    sidebarVisibility = .automatic
  }
}

/// Routes between login and registration screens.
struct AuthRouter: View {
  @State private var showRegister = false

  var body: some View {
    if showRegister {
      RegisterView(
        onSwitchToLogin: { showRegister = false }
      )
    } else {
      LoginView(
        onSwitchToRegister: { showRegister = true }
      )
    }
  }
}

private extension Comparable {
  /// Clamp `self` into a closed range. Used by the chat panel
  /// width logic so drag deltas can't push the persisted width
  /// outside `[chatMinWidth, chatMaxWidth]`.
  func clamped(to range: ClosedRange<Self>) -> Self {
    min(max(self, range.lowerBound), range.upperBound)
  }
}
