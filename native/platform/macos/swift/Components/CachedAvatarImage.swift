import AppKit
import SwiftUI

/// Shared, in-memory cache of decoded avatar images keyed by URL.
/// SwiftUI's `AsyncImage` caches raw bytes via `URLCache.shared` but
/// not the decoded `Image` value, so every new `AsyncImage` instance
/// flashes its placeholder while it re-decodes from disk. The
/// ProfileBar is present the whole session and decodes the avatar
/// once; when the user opens the profile dialog it creates a second
/// view that would redundantly decode the same URL. This cache lets
/// both read from the same decoded `NSImage`.
final class AvatarImageCache: @unchecked Sendable {
    static let shared = AvatarImageCache()

    private let cache: NSCache<NSURL, NSImage> = {
        let c = NSCache<NSURL, NSImage>()
        c.countLimit = 32
        return c
    }()

    func image(for url: URL) -> NSImage? {
        cache.object(forKey: url as NSURL)
    }

    func store(_ image: NSImage, for url: URL) {
        cache.setObject(image, forKey: url as NSURL)
    }

    /// Drop every cached avatar. Call on user switch so a new
    /// account doesn't briefly render the previous user's image.
    func clear() {
        cache.removeAllObjects()
    }
}

/// Drop-in replacement for `AsyncImage(url:)` backed by
/// `AvatarImageCache`. Renders synchronously from the shared cache
/// on first frame when a decoded image is already available — no
/// placeholder flash — and otherwise fetches via `URLSession.shared`
/// and populates the cache for next time.
struct CachedAvatarImage<Placeholder: View>: View {
    let url: URL?
    @ViewBuilder let placeholder: () -> Placeholder

    @State private var loadedImage: NSImage?

    init(url: URL?, @ViewBuilder placeholder: @escaping () -> Placeholder) {
        self.url = url
        self.placeholder = placeholder
        // Prime the state synchronously from the shared cache so
        // this view renders the image on its very first body pass
        // when the avatar has already been decoded elsewhere (e.g.
        // ProfileBar is open and the user taps to reveal the
        // profile dialog).
        if let url, let cached = AvatarImageCache.shared.image(for: url) {
            _loadedImage = State(initialValue: cached)
        }
    }

    var body: some View {
        Group {
            if let image = loadedImage {
                Image(nsImage: image).resizable().scaledToFill()
            } else {
                placeholder()
            }
        }
        .task(id: url) { await load() }
    }

    private func load() async {
        guard let url else {
            loadedImage = nil
            return
        }
        if let cached = AvatarImageCache.shared.image(for: url) {
            loadedImage = cached
            return
        }
        do {
            let (data, _) = try await URLSession.shared.data(from: url)
            guard !Task.isCancelled, let image = NSImage(data: data) else { return }
            AvatarImageCache.shared.store(image, for: url)
            loadedImage = image
        } catch {
            // Leave the placeholder in place on failure.
        }
    }
}
