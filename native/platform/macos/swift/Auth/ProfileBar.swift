import AppKit
import SwiftUI

/// Bottom-of-sidebar account bar. Translucent footer with the
/// user's avatar, display name, and current organization.
/// Clicking it opens a menu with Organization switcher, Settings…,
/// and Sign Out.
///
/// The menu opens *upward* (anchored to the top of the bar) so it
/// doesn't cover the sidebar content below when the bar sits at
/// the bottom of the sidebar. SwiftUI's `Menu` places its dropdown
/// in the default downward direction and has no API knob to flip
/// it; we bridge to `NSMenu.popUp(positioning:at:in:)` with the
/// last menu item as the positioning anchor, which places that
/// item at the chosen point and extends the menu upward from
/// there.
///
/// Uses `.glassEffect(.regular)` on macOS 26+ (native Liquid Glass)
/// with a `.thinMaterial` fallback on older macOS.
///
/// Accepts the fields it renders as plain value types (URL, String)
/// rather than a Firebase `User` reference — SwiftUI's view-diff
/// doesn't reliably re-render when a same-reference reference type
/// mutates in place, which was the source of the "display name
/// didn't update" bug we hit on the profile page.
///
/// Lives in its own file so the Windows shell (WinUI 3) can provide a
/// parallel implementation without reaching into monolithic SwiftUI.
struct ProfileBar: View {
    let photoURL: URL?
    let displayName: String?
    let email: String?
    let onSettings: () -> Void
    let onSecurity: () -> Void
    let onSignOut: () -> Void

    private static let avatarSize: CGFloat = 32

    /// Anchor for positioning the AppKit `NSMenu`. Captured from
    /// the invisible `MenuAnchorCapture` background view.
    @State private var anchorView: NSView?

    /// Holds the menu's action target so it isn't deallocated
    /// before the click lands. NSMenuItem only weakly references
    /// its target; a `let` inside the action would die before the
    /// user selects an item.
    @State private var menuActionTarget: MenuActionTarget?

    @Environment(\.pivoxTheme) private var theme

    var body: some View {
        Button(action: showUpwardMenu) {
            HStack(spacing: 10) {
                ProfileBarAvatar(photoURL: photoURL, size: Self.avatarSize)
                VStack(alignment: .leading, spacing: 1) {
                    Text(displayLabel)
                        .font(.body)
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                        .truncationMode(.tail)
                    Text(OrgService.shared.current?.displayName ?? "")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(ProfileBarButtonStyle())
        .background(MenuAnchorCapture { self.anchorView = $0 })
        .modifier(ProfileBarGlassBackground())
        .accessibilityIdentifier("sidebar-profile-bar")
    }

    private var displayLabel: String {
        if let name = displayName, !name.isEmpty { return name }
        if let email = email, !email.isEmpty { return email }
        return "Signed in"
    }

    private func showUpwardMenu() {
        guard let anchor = anchorView else { return }
        let target = MenuActionTarget(
            onSettings: onSettings,
            onSecurity: onSecurity,
            onSignOut: onSignOut,
            onSwitchOrg: { id in OrgService.shared.switchTo(id) })
        menuActionTarget = target

        let menu = NSMenu()

        // Organization submenu — only shown when the user has more
        // than one org to switch between. With a single membership
        // the menu would be a no-op.
        let orgs = OrgService.shared
        if orgs.all.count > 1 {
            let orgSubmenu = NSMenu()
            for org in orgs.all {
                let item = NSMenuItem(
                    title: org.displayName,
                    action: #selector(MenuActionTarget.switchOrganization(_:)),
                    keyEquivalent: "")
                item.target = target
                item.representedObject = org.id
                if org.id == orgs.current?.id { item.state = .on }
                orgSubmenu.addItem(item)
            }
            let orgItem = NSMenuItem(title: "Organization", action: nil, keyEquivalent: "")
            orgItem.submenu = orgSubmenu
            menu.addItem(orgItem)

            menu.addItem(.separator())
        }

        // No ⌘, key equivalent on this item: it always opens
        // Account, but the global ⌘, opens whichever tab was used
        // last. Showing ⌘, would imply they behave the same.
        let settings = NSMenuItem(
            title: "Settings…",
            action: #selector(MenuActionTarget.openSettings),
            keyEquivalent: "")
        settings.target = target
        menu.addItem(settings)

        let security = NSMenuItem(
            title: "Security…",
            action: #selector(MenuActionTarget.openSecurity),
            keyEquivalent: "")
        security.target = target
        menu.addItem(security)

        menu.addItem(.separator())

        let signOut = NSMenuItem(
            title: "Sign Out",
            action: #selector(MenuActionTarget.openSignOut),
            keyEquivalent: "")
        signOut.target = target
        menu.addItem(signOut)

        // Compute the button's top-left in *screen* coordinates so
        // we can hand an unambiguous point to NSMenu — view-coord
        // variants have to reason about NSView.isFlipped, which
        // was the source of the previous "opens at cursor"
        // behavior. With `in: nil`, `at:` is interpreted as
        // screen coordinates (origin bottom-left, y grows up).
        guard let window = anchor.window else { return }
        let buttonInWindow = anchor.convert(anchor.bounds, to: nil)
        let buttonInScreen = window.convertToScreen(buttonInWindow)
        // `positioning: menu.items.last` places the last item's
        // *top* at the given point. We want the last item's
        // *bottom* at the button's top so the full menu sits
        // above the bar — shift the point up by one item height
        // in screen coords (y grows up, so add). 22pt is the
        // standard NSMenu row height; small mismatches with the
        // actual height result in a ~1pt gap or overlap that's
        // below visual threshold.
        let itemHeight: CGFloat = 22
        let anchorPoint = NSPoint(
            x: buttonInScreen.minX,
            y: buttonInScreen.maxY + itemHeight)
        menu.popUp(
            positioning: menu.items.last,
            at: anchorPoint,
            in: nil)
    }
}

/// Obj-C bridge target for the NSMenu items. SwiftUI closures
/// can't be used as NSMenuItem actions directly — NSMenuItem
/// expects a `Selector` and a target. This tiny class exposes
/// `@objc` methods that wrap the SwiftUI-level closures.
private final class MenuActionTarget: NSObject {
    let onSettings: () -> Void
    let onSecurity: () -> Void
    let onSignOut: () -> Void
    let onSwitchOrg: (String) -> Void

    init(
        onSettings: @escaping () -> Void,
        onSecurity: @escaping () -> Void,
        onSignOut: @escaping () -> Void,
        onSwitchOrg: @escaping (String) -> Void
    ) {
        self.onSettings = onSettings
        self.onSecurity = onSecurity
        self.onSignOut = onSignOut
        self.onSwitchOrg = onSwitchOrg
    }

    @objc func openSettings() { onSettings() }
    @objc func openSecurity() { onSecurity() }
    @objc func openSignOut() { onSignOut() }

    @objc func switchOrganization(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String else { return }
        onSwitchOrg(id)
    }
}

/// Invisible 0-sized NSView that runs a callback with itself on
/// creation. Used as a `.background` behind the ProfileBar so we
/// have an NSView anchor in the AppKit world for positioning the
/// NSMenu. The captured view's geometry tracks the button because
/// SwiftUI places backgrounds in the same frame as the foreground.
private struct MenuAnchorCapture: NSViewRepresentable {
    let onCreated: (NSView) -> Void

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async { onCreated(view) }
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {}
}

/// Glass background for the bar — native Liquid Glass on macOS 26+,
/// `.thinMaterial` fallback elsewhere.
private struct ProfileBarGlassBackground: ViewModifier {
    @ViewBuilder
    func body(content: Content) -> some View {
        if #available(macOS 26.0, *) {
            content.glassEffect(.regular, in: Rectangle())
        } else {
            content.background(.thinMaterial)
        }
    }
}

/// Small circular avatar for the bar. Falls back to an SF Symbol when
/// the user has no photo or the image is still loading.
private struct ProfileBarAvatar: View {
    let photoURL: URL?
    let size: CGFloat

    var body: some View {
        CachedAvatarImage(url: photoURL) {
            Image(systemName: "person.crop.circle.fill")
                .resizable()
                .scaledToFit()
                .foregroundStyle(.secondary)
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .overlay(
            Circle().strokeBorder(Color.secondary.opacity(0.25), lineWidth: 0.5)
        )
    }
}

/// Pressed state only (no hover). Full-rect hit area — we can't use
/// `.buttonStyle(.plain)` because it bridges to an NSButton whose
/// hit-test skips transparent padding.
private struct ProfileBarButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed
                ? Color.secondary.opacity(0.15)
                : Color.clear)
    }
}
