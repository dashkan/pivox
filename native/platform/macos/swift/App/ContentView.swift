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
  @State private var showAIChat: Bool
  @State private var chatPanelWidth: CGFloat
  @State private var showingProfile = false
  private var auth = AuthService.shared
  private let appState = AppStateBridge.shared()

  /// Clamp range for the persisted chat-panel width so malformed /
  /// stale values don't produce unusable UIs. Matches the frame bounds
  /// we pass to the panel.
  private static let chatMinWidth: CGFloat = 320
  private static let chatMaxWidth: CGFloat = 500
  private static let chatDefaultWidth: CGFloat = 400

  init() {
    let state = AppStateBridge.shared()

    let saved = state.loadString(forKey: "selected_section")
    if let saved, let section = AppSection(rawValue: saved) {
      _selectedItem = State(initialValue: .section(section))
    } else {
      _selectedItem = State(initialValue: .section(.playoutOperator))
    }

    // Chat panel: toggle state + width restored across launches.
    _showAIChat = State(initialValue: state.hasBool(forKey: "ai_chat_open")
        ? state.loadBool(forKey: "ai_chat_open") : false)

    let savedWidth = state.loadString(forKey: "ai_chat_panel_width")
      .flatMap { Double($0) }.map { CGFloat($0) } ?? Self.chatDefaultWidth
    _chatPanelWidth = State(initialValue: min(max(savedWidth, Self.chatMinWidth), Self.chatMaxWidth))
  }

  var body: some View {
    Group {
      if auth.isSignedIn {
        mainAppView
      } else {
        AuthRouter()
      }
    }
    .onChange(of: selectedItem) { _, newValue in
      if case .section(let section) = newValue {
        appState.save(section.rawValue, forKey: "selected_section")
      }
    }
    .onChange(of: showAIChat) { _, isOpen in
      appState.save(isOpen, forKey: "ai_chat_open")
    }
    .onReceive(
      NotificationCenter.default.publisher(for: DelegatedAuthCoordinator.openProfileNotification)
    ) { _ in
      showingProfile = true
    }
  }

  /// Prefer the user's custom photo, fall back to any linked provider's
  /// photo (Google/GitHub/etc). When a user signs in via Google and
  /// hasn't set a custom photo, Firebase doesn't always mirror Google's
  /// avatar into `user.photoURL` — we dig it out of `providerData`
  /// ourselves so the bar has a picture instead of a silhouette.
  private var effectivePhotoURL: URL? {
    if let url = auth.currentUser?.photoURL { return url }
    return auth.currentUser?.providerData
      .compactMap(\.photoURL)
      .first
  }

  private var mainAppView: some View {
    NavigationSplitView(columnVisibility: $sidebarVisibility) {
      SidebarNavList(selectedItem: $selectedItem)
      .navigationSplitViewColumnWidth(min: 180, ideal: 220, max: 300)
      .safeAreaInset(edge: .bottom, spacing: 0) {
        ProfileBar(
          photoURL: effectivePhotoURL,
          displayName: auth.currentUser?.displayName,
          email: auth.currentUser?.email,
          action: { showingProfile = true }
        )
      }
    } detail: {
      detailContent
    }
    .sheet(isPresented: $showingProfile) {
      // Fixed frame so switching between Account / Security tabs
      // doesn't resize the dialog to fit each page's content.
      ProfileView()
        .frame(width: 720, height: 620)
    }
    .background {
      // Hotkey target. Lives outside the toolbar button so that
      // Space-to-activate on the toolbar button and Cmd+Shift+A
      // go through separate code paths — the hotkey opens AND asks
      // the chat view to grab focus; Space on the button only
      // toggles visibility, leaving focus on the button itself.
      Button {
        let willOpen = !showAIChat
        withAnimation(.easeInOut(duration: 0.2)) {
          showAIChat = willOpen ? true : false
        }
        if willOpen {
          NotificationCenter.default.post(name: .aiChatFocusRequested, object: nil)
        }
      } label: {
        EmptyView()
      }
      .keyboardShortcut("a", modifiers: [.command, .shift])
      .buttonStyle(.plain)
      .frame(width: 0, height: 0)
      .opacity(0)
      .accessibilityHidden(true)
      // Keep the button out of the Tab loop — otherwise focus hops
      // onto this invisible element and user sees "Tab did nothing".
      .focusable(false)
    }
    .toolbar {
      ToolbarItem(placement: .primaryAction) {
        Button {
          withAnimation(.easeInOut(duration: 0.2)) {
            showAIChat.toggle()
          }
        } label: {
          Image(systemName: showAIChat
            ? "bubble.left.and.text.bubble.right.fill"
            : "bubble.left.and.text.bubble.right")
        }
        .help("Toggle AI Chat (⌘⇧A)")
      }
    }
    .onChange(of: isImageEditing) { _, editing in
      NSApp.keyWindow?.appearance =
        editing
        ? NSAppearance(named: .darkAqua)
        : nil
    }
  }

  @ViewBuilder
  private var detailContent: some View {
    HSplitView {
      // Main content area. `maxWidth: .infinity` locks in a stable
      // ideal width across sections — without it, HSplitView sees
      // different intrinsic widths per section (e.g. Library's button
      // is wider than the placeholder texts) and re-splits the chat
      // panel accordingly when you switch sidebar items.
      mainDetail
        .frame(minWidth: 400, maxWidth: .infinity)

      // AI Chat panel — slides in from right. Width persists across
      // launches: initial size from AppState at mount. Subsequent
      // user drags are observed via a GeometryReader and persisted to
      // AppState, but we deliberately do NOT write back into
      // `chatPanelWidth` at runtime. Doing so creates a feedback loop
      // with `idealWidth`: GeometryReader observes width → updates
      // state → idealWidth changes → HSplitView rebalances → observed
      // width changes again. That loop was visible as the panel
      // drifting every time the sidebar selection changed, because a
      // content swap triggers HSplitView to re-propose sizes.
      if showAIChat {
        AIChatContainerView()
          .frame(minWidth: Self.chatMinWidth,
                 idealWidth: chatPanelWidth,
                 maxWidth: Self.chatMaxWidth)
          .background(
            GeometryReader { proxy in
              Color.clear.onChange(of: proxy.size.width) { _, newWidth in
                guard newWidth >= Self.chatMinWidth,
                      newWidth <= Self.chatMaxWidth else { return }
                // Persist only — no runtime state update (see above).
                appState.save(String(Double(newWidth)),
                              forKey: "ai_chat_panel_width")
              }
            }
          )
          .transition(.move(edge: .trailing))
      }
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
