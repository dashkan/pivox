import AppKit
import SwiftUI

enum AppSection: String, CaseIterable, Identifiable {
  case playoutOperator = "Operator"
  case library = "Library"
  case designer = "Designer"
  case engineering = "Engineering"
  case aiChat = "AI Chat"
  case admin = "Admin"

  var id: String { rawValue }

  var icon: String {
    switch self {
    case .playoutOperator: return "play.rectangle"
    case .library: return "photo.on.rectangle"
    case .designer: return "paintbrush"
    case .engineering: return "wrench.and.screwdriver"
    case .aiChat: return "bubble.left.and.text.bubble.right"
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
  private var auth = AuthService.shared
  private let appState = AppStateBridge.shared()

  init() {
    let saved = AppStateBridge.shared().loadString(forKey: "selected_section")
    if let saved, let section = AppSection(rawValue: saved) {
      _selectedItem = State(initialValue: .section(section))
    } else {
      _selectedItem = State(initialValue: .section(.playoutOperator))
    }
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
    // Delegated auth (AUTHN-07): when the plugin deep-links into
    // `pivox://auth/delegate/profile`, the coordinator posts this
    // notification and the main ContentView swings the sidebar to Profile.
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
      switch selectedItem {
      case .section(let section):
        if section == .library {
          LibraryPlaceholderView(
            isEditing: $isImageEditing,
            sidebarVisibility: $sidebarVisibility
          )
        } else if section == .aiChat {
          AIChatContainerView()
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
    .onChange(of: isImageEditing) { _, editing in
      NSApp.keyWindow?.appearance =
        editing
        ? NSAppearance(named: .darkAqua)
        : nil
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
        // UI test hook: auto-load a test image to bypass NSOpenPanel.
        // Only on first appearance — not after Done/Back returns here.
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
