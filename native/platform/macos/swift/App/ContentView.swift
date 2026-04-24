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
  @State private var sidebarWidth: CGFloat
  @State private var showingProfile = false
  @State private var aiToggleHovered = false
  private var auth = AuthService.shared
  private let appState = AppStateBridge.shared()

  /// Clamp range for the persisted chat-panel width so malformed /
  /// stale values don't produce unusable UIs. Matches the frame bounds
  /// we pass to the panel.
  private static let chatMinWidth: CGFloat = 320
  private static let chatMaxWidth: CGFloat = 500
  private static let chatDefaultWidth: CGFloat = 400

  /// Clamp range for the persisted sidebar width. Matches the
  /// `navigationSplitViewColumnWidth(min:ideal:max:)` bounds.
  private static let sidebarMinWidth: CGFloat = 180
  private static let sidebarMaxWidth: CGFloat = 300
  private static let sidebarDefaultWidth: CGFloat = 220

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

    // Sidebar width: persist across launches so a wider / narrower
    // sidebar chosen by the user sticks. Same load-clamp-init shape
    // as the chat panel above.
    let savedSidebarWidth = state.loadString(forKey: "sidebar_width")
      .flatMap { Double($0) }.map { CGFloat($0) } ?? Self.sidebarDefaultWidth
    _sidebarWidth = State(initialValue:
      min(max(savedSidebarWidth, Self.sidebarMinWidth), Self.sidebarMaxWidth))
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

  private var mainAppView: some View {
    // Chat panel lives as a PEER of NavigationSplitView, not inside
    // its detail column. This lets NavigationSplitView negotiate
    // sidebar-vs-detail cleanly (where it's well-behaved) while the
    // outer HSplitView handles main-vs-chat (where HSplitView is
    // well-behaved). The previous structure nested HSplitView inside
    // NavigationSplitView's detail, and HSplitView's width demands
    // couldn't propagate up — so opening chat silently crushed the
    // sidebar below its own declared min.
    HSplitView {
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
              action: { showingProfile = true }
            )
          }
      } detail: {
        mainDetail
      }
      // `maxWidth: .infinity` is the expanding side so the chat
      // panel's `idealWidth` drives the split position. Without it,
      // the HSplitView re-negotiates when a sidebar section change
      // shifts `mainDetail`'s intrinsic width (Library's button is
      // wider than the other sections' text), which used to drift
      // the chat panel width.
      //
      // `minWidth: 580` = sidebar min (180) + mainDetail min (400),
      // ensuring the NavigationSplitView side of the outer split is
      // never crushed below a width that fits both its columns.
      .frame(minWidth: 580, maxWidth: .infinity)

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
                appState.save(String(Double(newWidth)),
                              forKey: "ai_chat_panel_width")
              }
            }
          )
          .transition(.move(edge: .trailing))
      }
    }
    .toolbar {
      // Applied on the outer HSplitView (the thing that spans the
      // whole window) rather than on NavigationSplitView, so
      // `.primaryAction` actually lands at the window's right edge.
      // When the toolbar was on NavigationSplitView it anchored to
      // the sidebar-plus-detail half only, putting the chat toggle
      // in the middle of the window.
      ToolbarItem(placement: .primaryAction) {
        Button {
          withAnimation(.easeInOut(duration: 0.2)) {
            showAIChat.toggle()
          }
        } label: {
          Image(systemName: "sparkles")
            .pivoxIconToolbar()
            .symbolVariant(showAIChat ? .fill : .none)
            .aiShimmerSymbol(isActive: aiToggleHovered)
            .padding(6)
            .contentShape(Rectangle())
            .onHover { aiToggleHovered = $0 }
        }
        // `.plain` drops the default toolbar-button chrome, which
        // includes the subtle gray hover highlight AppKit overlays
        // on top of the icon. We replace its job with our own icon
        // shimmer on hover — "cursor is here" without the gray ring
        // competing with the sparkles glow.
        .buttonStyle(.plain)
        // Mouse-click-only side-effect: when opening via pointer,
        // also steal focus to the message input. `TapGesture` fires
        // for mouse/touch but NOT for keyboard Space-activation on
        // a focused button, so a keyboard user Tab'ing around the
        // UI can toggle the panel without losing their focus
        // position. The deferred check reads `showAIChat` after all
        // synchronous gesture handlers have run, so whichever order
        // SwiftUI dispatches them, we only post when the panel is
        // actually now open.
        .simultaneousGesture(
          TapGesture().onEnded {
            DispatchQueue.main.async {
              if showAIChat {
                NotificationCenter.default.post(name: .aiChatFocusRequested, object: nil)
              }
            }
          }
        )
        .help("Toggle AI Chat (⌘⇧A)")
      }
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
    .onChange(of: isImageEditing) { _, editing in
      NSApp.keyWindow?.appearance =
        editing
        ? NSAppearance(named: .darkAqua)
        : nil
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
