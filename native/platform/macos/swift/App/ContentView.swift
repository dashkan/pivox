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
  case profile
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
      selectedItem = .profile
    }
  }

  private var mainAppView: some View {
    NavigationSplitView(columnVisibility: $sidebarVisibility) {
      List(selection: $selectedItem) {
        ForEach(AppSection.allCases) { section in
          Label(section.rawValue, systemImage: section.icon)
            .tag(SidebarItem.section(section))
        }
      }
      .listStyle(.sidebar)
      .navigationSplitViewColumnWidth(min: 180, ideal: 220, max: 300)
      .safeAreaInset(edge: .bottom, spacing: 0) {
        VStack(spacing: 0) {
          Divider()
          Button(action: { selectedItem = .profile }) {
            Label("Profile", systemImage: "person.circle")
              .frame(maxWidth: .infinity, alignment: .leading)
              .padding(.horizontal, 16)
              .padding(.vertical, 10)
              .background(
                selectedItem == .profile
                  ? Color.accentColor
                  : Color.clear,
                in: RoundedRectangle(cornerRadius: 6)
              )
              .foregroundStyle(selectedItem == .profile ? .white : .primary)
          }
          .buttonStyle(.plain)
          .padding(.horizontal, 8)
          .padding(.vertical, 8)
        }
      }
    } detail: {
      detailContent
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
        .keyboardShortcut("a", modifiers: [.command, .shift])
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
      // Main content area
      mainDetail
        .frame(minWidth: 400)

      // AI Chat panel — slides in from right. Width persists across
      // launches: initial size from AppState, subsequent user drags
      // captured via a GeometryReader overlay that records the actual
      // rendered width into AppState (debounced by change-on-value).
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
                // Only persist substantive changes — avoids flooding
                // storage on every layout pass during a drag.
                if abs(newWidth - chatPanelWidth) >= 4 {
                  chatPanelWidth = newWidth
                  appState.save(String(Double(newWidth)),
                                forKey: "ai_chat_panel_width")
                }
              }
            }
          )
          .transition(.move(edge: .trailing))
      }
    }
  }

  @ViewBuilder
  private var mainDetail: some View {
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
        .frame(maxWidth: .infinity, maxHeight: .infinity)
      }
    case .profile:
      ProfileView()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    case nil:
      Text("Select a section")
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
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
