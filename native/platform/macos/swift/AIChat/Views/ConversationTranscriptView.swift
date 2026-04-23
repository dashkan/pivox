import AppKit
import Combine
import PivoxModels
import SwiftUI

/// AppKit-backed transcript view. `NSScrollView` + `NSTableView` with
/// per-cell `NSHostingView` rendering the SwiftUI `Message` component.
///
/// # Why AppKit, not SwiftUI
///
/// SwiftUI's `LazyVStack` has an unresolvable conflict with scroll
/// anchoring: the anchor resolves before lazy rows materialize, so
/// `.defaultScrollAnchor(.bottom)` lands in a stub height and scroll
/// preservation on prepend jumps to the top. Eager `VStack` preserves
/// scroll correctly but holds every fully-rendered message in memory
/// (1000 messages ≈ 800MB resident with rich markdown). `NSTableView`
/// gives us windowed rendering AND deterministic scroll control; 1000
/// messages sit at ~80MB resident, and prepend scroll preservation is
/// a straightforward "measure inserted rows, shift origin" calculation.
///
/// # Why NOT the prior AppKit attempt (see git history)
///
/// The first attempt enabled `usesAutomaticRowHeights = true` even
/// though that file's own comments warned it under-measures with
/// `NSHostingView` intrinsic sizes. It then added a runtime mismatch
/// logger instead of fixing the measurement. That's the canonical
/// mistake this file is designed NOT to repeat: if a primitive is
/// documented as unreliable for your use case, don't build on it and
/// instrument the bug — replace the primitive.
///
/// Everything here is built phase-by-phase, one verifiable step at a
/// time:
///   1. Render + scroll-to-bottom on first load.
///   2. Explicit measurement + resize invalidation.
///   3. Pagination with scroll preservation.
///   4. Lazy width-aware height refresh (resize stays interactive on
///      large conversations).
///
/// Streaming, bottom-pin-when-short, edit/regenerate callbacks, and
/// nested scroll-event forwarding are deliberately NOT in this file
/// yet. They layer in as subsequent phases with their own gates.
///
/// # Why DEBUG page boundaries
///
/// Visual "Page N" dividers between loaded pages make pagination
/// issues obvious at a glance (wrong boundary count, stuck page,
/// cascading loads). Gated behind `#if DEBUG` so release builds have
/// no boundary rows, no boundary measurement cost, and no boundary
/// accounting in the prepend shift math.
struct ConversationTranscriptView: NSViewRepresentable {
    @ObservedObject var viewModel: ConversationViewModel

    func makeCoordinator() -> Coordinator {
        Coordinator(viewModel: viewModel)
    }

    func makeNSView(context: Context) -> NSScrollView {
        let scrollView = NSScrollView()
        scrollView.hasVerticalScroller = true
        scrollView.drawsBackground = false
        scrollView.borderType = .noBorder

        let tableView = NSTableView()
        tableView.headerView = nil
        tableView.backgroundColor = .clear
        tableView.selectionHighlightStyle = .none
        tableView.gridStyleMask = []
        // `.custom` row sizing → we own `tableView(_:heightOfRow:)`.
        // We deliberately do NOT use `usesAutomaticRowHeights`; it
        // calls `NSHostingView.fittingSize` which under-measures rich
        // SwiftUI content (markdown, code blocks, lists), producing
        // cells that clip their own content. This was the fatal flaw
        // in the prior AppKit attempt.
        tableView.rowSizeStyle = .custom
        tableView.intercellSpacing = NSSize(width: 0, height: 8)

        let column = NSTableColumn(identifier: .messageColumn)
        // Auto-resizing so the column width tracks the scroll view's
        // content width. Our measurement functions key off this
        // width, so keeping it in sync with the viewport is critical.
        column.resizingMask = .autoresizingMask
        tableView.addTableColumn(column)

        tableView.dataSource = context.coordinator
        tableView.delegate = context.coordinator
        context.coordinator.tableView = tableView
        context.coordinator.scrollView = scrollView

        scrollView.documentView = tableView

        // Panel resize → column width changes → row heights must be
        // recomputed against the new width for visible content. We
        // listen on the scroll view (not the content view) because
        // the scroll view's frame is what SwiftUI's parent layout
        // adjusts; content view bounds change on scroll as well as
        // resize, so they're too noisy for width-change detection.
        scrollView.postsFrameChangedNotifications = true
        NotificationCenter.default.addObserver(
            context.coordinator,
            selector: #selector(Coordinator.scrollViewFrameDidChange(_:)),
            name: NSView.frameDidChangeNotification,
            object: scrollView)

        // Content view bounds change on every user scroll. We use
        // this for two purposes: (a) fire load-older when the user
        // approaches the top, and (b) refresh heights of any rows
        // that scrolled into view at a new width since last
        // measurement (lazy post-resize fixup).
        scrollView.contentView.postsBoundsChangedNotifications = true
        NotificationCenter.default.addObserver(
            context.coordinator,
            selector: #selector(Coordinator.contentViewBoundsDidChange(_:)),
            name: NSView.boundsDidChangeNotification,
            object: scrollView.contentView)

        return scrollView
    }

    /// `updateNSView` fires whenever an observed property on
    /// `viewModel` changes. We read `viewModel.messages` synchronously
    /// here — SwiftUI has already committed the new value before
    /// calling us, so `sync` sees the latest state.
    ///
    /// We specifically do NOT subscribe to `$messages` via Combine
    /// with `.receive(on: RunLoop.main)`. That makes the sink
    /// asynchronous, which means it runs AFTER the ViewModel's
    /// `defer { isLoadingOlder = false }` has executed on `loadOlder`.
    /// Code that wanted to observe "isLoadingOlder is still true
    /// during sync" then broke. `updateNSView` is synchronous and
    /// deterministic — no scheduling hacks needed.
    func updateNSView(_ scrollView: NSScrollView, context: Context) {
        context.coordinator.sync(viewModel: viewModel)
    }

    @MainActor
    final class Coordinator: NSObject, NSTableViewDataSource, NSTableViewDelegate {
        var viewModel: ConversationViewModel
        weak var tableView: NSTableView?
        weak var scrollView: NSScrollView?

        /// Rendered row model. Messages interleave with page-boundary
        /// markers in DEBUG builds. Kept in display order (oldest at
        /// index 0, newest at the end) because NSTableView indexes
        /// rows top-to-bottom and we want the newest at the bottom.
        ///
        /// A single `[Row]` is simpler than parallel arrays (one for
        /// messages, one for boundaries): `numberOfRows`,
        /// `heightOfRow`, and `viewFor` all need the same linear
        /// sequence, and keeping one array keeps the indexing math
        /// trivial.
        private enum Row {
            case message(Pivox_Ai_V1_Message)
            case boundary(page: Int)
        }
        private var rows: [Row] = []
        private var messages: [Pivox_Ai_V1_Message] = []

        /// Size of each loaded page in display order (oldest first).
        /// Initial load is `[initialCount]`. Each `loadOlder` inserts
        /// the new count at index 0 (older pages are above). We track
        /// this separately from `messages` so `rebuildRows` knows
        /// where to insert boundary markers.
        private var pageSizes: [Int] = []

        /// Height cache keyed by row identity (`m:<name>` for
        /// messages, `b:<page>` for boundaries). Persists across
        /// `reloadData` calls because row identity is stable — a
        /// prepend doesn't change existing message names, it just
        /// adds new ones above.
        private var heightCache: [String: CGFloat] = [:]
        private var cachedWidth: CGFloat = 0

        /// Blocks pagination triggers until the initial scroll-to-
        /// bottom completes. Without it, the brief window between
        /// "first page lands at the top" and "scrollRowToVisible
        /// takes effect" has `minY < 300`, which would fire loadOlder
        /// instantly and produce a spurious second-page fetch on
        /// first open.
        private var hasScrolledToBottomOnce = false

        /// Latches between "fired loadOlder" and "prepend applied or
        /// end-of-history detected". Critical because:
        ///   (a) `viewModel.isLoadingOlder` flips back to false
        ///       inside `loadOlder`'s `defer`, which happens BEFORE
        ///       our `sync()` sees the new messages — so we can't
        ///       rely on that flag in the prepend path.
        ///   (b) Without a latch, bounds-change events fired during
        ///       the loading window would re-trigger another
        ///       loadOlder before the first one's prepend lands,
        ///       cascading into a pull of every page at once.
        private var expectingPrepend = false

        /// Reused off-screen host for row-height measurement.
        /// Creating a fresh `NSHostingController` per measurement is
        /// expensive (it instantiates an entire SwiftUI render tree);
        /// reusing one and swapping `rootView` is ~100x cheaper.
        private let sizingHost = NSHostingController(rootView: AnyView(EmptyView()))

        /// Width at which each row's height in `heightCache` is
        /// valid. Rows whose entry here doesn't match the current
        /// effective width are stale and get re-measured the next
        /// time they become visible. This is how we avoid the
        /// expensive "note all rows" sweep on resize — offscreen
        /// rows are lazily refreshed as they scroll into view.
        ///
        /// The naive alternative (clear heightCache on every resize
        /// tick and call `noteHeightOfRows(withIndexesChanged:
        /// IndexSet(0..<rows.count))`) forces synchronous
        /// remeasurement of every row, which beachballs for ~1s on a
        /// 1000-message conversation.
        private var lastNotedWidth: [String: CGFloat] = [:]

        init(viewModel: ConversationViewModel) {
            self.viewModel = viewModel
            super.init()
        }

        // MARK: - Sync

        func sync(viewModel: ConversationViewModel) {
            self.viewModel = viewModel
            applyMessages(viewModel.messages)
        }

        /// Dispatcher for mutations coming off `viewModel.messages`.
        /// We can't just `reloadData()` on every change because:
        ///   - First-load: need to populate, then scroll to bottom.
        ///   - Prepend (loadOlder): need to preserve scroll position
        ///     through the mutation, which requires measuring BEFORE
        ///     reloadData.
        ///   - Other mutations (future: streaming append): need
        ///     different handling (scroll-to-bottom vs preserve).
        /// So we classify the mutation first, then take the right
        /// path.
        private func applyMessages(_ new: [Pivox_Ai_V1_Message]) {
            // Cheap identity check — SwiftUI fires updateNSView for
            // reasons unrelated to our data (parent re-renders, size
            // changes, etc.). Bail fast when messages are unchanged.
            if messagesUnchanged(new) { return }

            let oldCount = messages.count
            let newCount = new.count
            let prependCount = detectPrependCount(old: messages, new: new)

            if oldCount == 0, newCount > 0 {
                // First page landed.
                messages = new
                pageSizes = [newCount]
                rebuildRows()
                tableView?.reloadData()
                if !hasScrolledToBottomOnce {
                    hasScrolledToBottomOnce = true
                    // Defer the scroll to next runloop tick: after
                    // reloadData, NSTableView needs a layout pass
                    // before row rects are valid. Calling
                    // scrollRowToVisible synchronously scrolls to
                    // stale (pre-layout) coordinates — you end up
                    // somewhere mid-content instead of the bottom.
                    DispatchQueue.main.async { [weak self] in
                        guard let self, !self.rows.isEmpty else { return }
                        self.tableView?.scrollRowToVisible(self.rows.count - 1)
                    }
                }
            } else if prependCount > 0 {
                // Older page prepended. Preserve scroll.
                applyPrepend(newMessages: new, prependCount: prependCount)
            } else if newCount != oldCount {
                // Any other size change (future: streaming append,
                // message edit, regenerate). Structural reload for
                // now — specialized handling layers in later with
                // its own phase gate.
                messages = new
                rebuildRowsKeepingPages()
                tableView?.reloadData()
            }

            if expectingPrepend, prependCount == 0, newCount == oldCount {
                // loadOlder returned nothing (end of history). Clear
                // the latch so subsequent scroll-to-top events can
                // still be processed (they'll be gated by
                // `canLoadOlder` which is now false anyway, but
                // leaving the latch set would be misleading state).
                expectingPrepend = false
            }
        }

        private func messagesUnchanged(_ new: [Pivox_Ai_V1_Message]) -> Bool {
            guard new.count == messages.count else { return false }
            // Compare by stable identity (`name`), not by full
            // content equality — message content is large (markdown
            // strings) and unchanging; name comparison is O(n) cheap.
            for i in 0..<new.count where new[i].name != messages[i].name {
                return false
            }
            return true
        }

        /// Returns the number of messages prepended at the head of
        /// `new` compared to `old`. Zero if not a pure prepend.
        ///
        /// We deliberately check that `new[delta].name == old.first`
        /// instead of just returning `new.count - old.count`. Reason:
        /// streaming appends also grow the count, but they shift the
        /// *tail* of the array, not the head. Without the head-
        /// alignment check we'd misclassify streaming appends as
        /// prepends and try to shift scroll on them — disastrous UX
        /// (streaming responses would scroll the transcript DOWN).
        private func detectPrependCount(
            old: [Pivox_Ai_V1_Message],
            new: [Pivox_Ai_V1_Message]
        ) -> Int {
            let delta = new.count - old.count
            guard delta > 0, !old.isEmpty else { return 0 }
            return new[delta].name == old.first?.name ? delta : 0
        }

        /// Called when `loadOlder` returned a non-empty page.
        ///
        /// The preservation algorithm is:
        ///   1. Measure every row about to be inserted (N new
        ///      messages + 1 boundary in DEBUG).
        ///   2. Sum measured heights plus intercell spacing — that's
        ///      exactly how far the existing content will be pushed
        ///      down by the insertion.
        ///   3. Capture current scroll origin.
        ///   4. Apply the new model and `reloadData`.
        ///   5. Scroll to `oldOrigin + insertedHeight` — the
        ///      previously-visible content is now at that offset, so
        ///      the user sees the exact same viewport.
        ///
        /// We do NOT use `insertRows(at:withAnimation:)` because it
        /// relies on NSTableView auto-recomputing heights via
        /// `heightOfRow` during the insert — which only works with
        /// `usesAutomaticRowHeights`, which we don't use. Full
        /// `reloadData` + explicit shift is deterministic and doesn't
        /// depend on NSTableView internals.
        ///
        /// We also do NOT measure via `tableView.frame.height` delta
        /// (the prior attempt's approach) because that requires
        /// `layoutSubtreeIfNeeded` to have recomputed heights before
        /// we read the frame — a timing dependency that's fragile.
        /// Measuring our own inputs before the mutation is purely
        /// data-driven.
        private func applyPrepend(
            newMessages: [Pivox_Ai_V1_Message],
            prependCount: Int
        ) {
            let width = effectiveWidth()
            let spacing = tableView?.intercellSpacing.height ?? 0

            // Measure every row being inserted above the current
            // content. Populating the cache here means subsequent
            // `heightOfRow` calls from NSTableView hit the cache
            // instead of re-measuring.
            var insertedHeight: CGFloat = 0
            var insertedRowCount = prependCount
            for i in 0..<prependCount {
                let msg = newMessages[i]
                let h = measureMessageHeight(msg, width: width)
                let key = "m:\(msg.name)"
                heightCache[key] = h
                lastNotedWidth[key] = width
                insertedHeight += h
            }
            #if DEBUG
            // Boundary row accounting only compiled into DEBUG
            // builds. Release builds have no boundary rows, so the
            // shift math doesn't include their height or their
            // spacing contribution.
            let newPageNumber = pageSizes.count + 1
            let boundaryH = measureBoundaryHeight(width: width)
            let boundaryKey = "b:\(newPageNumber)"
            heightCache[boundaryKey] = boundaryH
            lastNotedWidth[boundaryKey] = width
            insertedHeight += boundaryH
            insertedRowCount += 1
            #endif

            // Each inserted row contributes one unit of intercell
            // spacing above the next row, INCLUDING the spacing just
            // before the first previously-visible row (which used to
            // be at the top but is now preceded by the last inserted
            // row). That's `insertedRowCount` spacings total, not
            // `insertedRowCount - 1`.
            insertedHeight += CGFloat(insertedRowCount) * spacing

            // Capture scroll origin BEFORE mutation. After reloadData
            // the clip view's bounds.origin may change as AppKit
            // relayouts — capturing after would read the wrong value.
            let oldOrigin = scrollView?.contentView.bounds.origin ?? .zero

            // Apply the model.
            messages = newMessages
            pageSizes.insert(prependCount, at: 0)
            rebuildRows()
            tableView?.reloadData()

            // Shift scroll down by the inserted height so the
            // previously-visible content remains at the same viewport
            // y-coordinate. Happens in the same runloop tick as
            // reloadData, so no intermediate "scrolled to top" frame
            // is ever presented — the user sees the prepend appear
            // seamlessly above their current view.
            let newOrigin = NSPoint(x: oldOrigin.x, y: oldOrigin.y + insertedHeight)
            scrollView?.contentView.scroll(to: newOrigin)
            if let sv = scrollView {
                // `scroll(to:)` doesn't automatically sync the
                // scroll view's other subviews (rulers, scrollers).
                // `reflectScrolledClipView` tells the scroll view
                // to re-adopt the clip view's new origin.
                sv.reflectScrolledClipView(sv.contentView)
            }

            expectingPrepend = false
        }

        /// Flattens `pageSizes` + `messages` into the linear `rows`
        /// sequence NSTableView expects.
        ///
        /// Pages are ordered oldest-first in both `messages` (index 0
        /// is the oldest message) and `pageSizes` (index 0 is the
        /// oldest page). That matches NSTableView's top-to-bottom
        /// row indexing.
        private func rebuildRows() {
            rows.removeAll(keepingCapacity: true)
            var idx = 0
            for (pageIdx, size) in pageSizes.enumerated() {
                for _ in 0..<size {
                    rows.append(.message(messages[idx]))
                    idx += 1
                }
                #if DEBUG
                // Insert a boundary after every page except the
                // newest (bottom). Page number labels the content
                // ABOVE the boundary: pageSizes[0] is the oldest page
                // → highest page number = pageSizes.count.
                //
                // Example with 3 pages loaded:
                //   pageSizes = [50, 50, 50]
                //   page 3 msgs → boundary "Page 3" → page 2 msgs
                //   → boundary "Page 2" → page 1 msgs (newest, bottom)
                if pageIdx < pageSizes.count - 1 {
                    let pageNumber = pageSizes.count - pageIdx
                    rows.append(.boundary(page: pageNumber))
                }
                #endif
            }
        }

        /// For non-prepend mutations (future streaming/append path).
        /// Absorbs any count delta into the newest page so existing
        /// boundary positions stay correct.
        private func rebuildRowsKeepingPages() {
            if pageSizes.isEmpty {
                pageSizes = [messages.count]
            } else {
                let totalExpected = pageSizes.reduce(0, +)
                let delta = messages.count - totalExpected
                if delta != 0 {
                    pageSizes[pageSizes.count - 1] += delta
                }
            }
            rebuildRows()
        }

        // MARK: - Scroll observers

        @objc func scrollViewFrameDidChange(_ notification: Notification) {
            guard let scrollView else { return }
            let newWidth = scrollView.contentSize.width
            // Width changes trigger height invalidation; pure height
            // changes (window vertical resize) don't need
            // remeasurement because row heights depend only on width.
            guard abs(newWidth - cachedWidth) > 0.5 else { return }
            cachedWidth = newWidth
            refreshVisibleRowHeights()
        }

        @objc func contentViewBoundsDidChange(_ notification: Notification) {
            // Every scroll tick: catch any rows newly scrolled into
            // view whose heights were measured at an older width.
            // This is the companion to the resize handler's "only
            // refresh visible" — rows not visible during a resize
            // get refreshed here as they scroll into view.
            refreshVisibleRowHeights()

            // Pagination trigger: user scrolled near the top.
            guard hasScrolledToBottomOnce else { return }
            guard !expectingPrepend else { return }
            guard viewModel.canLoadOlder, !viewModel.isLoadingOlder else { return }
            guard let scrollView else { return }

            // 300pt threshold: far enough from the edge that the
            // load fires before the user hits a hard stop, close
            // enough that it doesn't fire on casual upward scrolls.
            let minY = scrollView.contentView.bounds.origin.y
            guard minY < 300 else { return }

            // Set the latch BEFORE dispatching the Task. If we set
            // it inside the Task, a second bounds-change could fire
            // a second loadOlder before the first Task runs its
            // await, racing to produce duplicate prepends.
            expectingPrepend = true
            Task { [weak self] in
                guard let self else { return }
                await self.viewModel.loadOlder()
            }
        }

        /// For each currently-visible row whose cached height was
        /// measured at a width different from the current effective
        /// width, invalidate the cache entry and note it so
        /// NSTableView re-queries at the new width. Called both
        /// during live resize (to update the visible portion) and
        /// during scroll (to catch newly-visible rows after a prior
        /// resize).
        ///
        /// This is THE optimization that makes resize interactive on
        /// large conversations. The naive "note all rows" approach
        /// does N synchronous measurements (≥5ms each for rich
        /// markdown), producing a 5+ second beachball on a 1000-
        /// message conversation. This version only measures the
        /// ~10 rows on screen, spreading the rest across scroll-
        /// time events.
        private func refreshVisibleRowHeights() {
            guard let tableView else { return }
            let visible = tableView.rows(in: tableView.visibleRect)
            guard visible.length > 0 else { return }
            let width = effectiveWidth(tableView: tableView)
            var toNote = IndexSet()
            let range = visible.location..<(visible.location + visible.length)
            for row in range where row >= 0 && row < rows.count {
                let key = cacheKey(for: rows[row])
                if lastNotedWidth[key] != width {
                    lastNotedWidth[key] = width
                    heightCache.removeValue(forKey: key)
                    toNote.insert(row)
                }
            }
            if !toNote.isEmpty {
                tableView.noteHeightOfRows(withIndexesChanged: toNote)
            }
        }

        // MARK: - NSTableViewDataSource

        func numberOfRows(in tableView: NSTableView) -> Int { rows.count }

        // MARK: - NSTableViewDelegate

        /// NSTableView calls this repeatedly during layout (every
        /// row, not just visible ones, to compute total content
        /// height for the scrollbar). Cache lookups must be fast;
        /// re-measurement happens only on miss (cache keyed by row
        /// id and re-checked against current width via
        /// `lastNotedWidth`).
        func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat {
            let width = effectiveWidth(tableView: tableView)
            let key = cacheKey(for: rows[row])
            if lastNotedWidth[key] == width, let cached = heightCache[key] {
                return cached
            }
            let measured: CGFloat
            switch rows[row] {
            case .message(let msg):
                measured = measureMessageHeight(msg, width: width)
            case .boundary:
                measured = measureBoundaryHeight(width: width)
            }
            heightCache[key] = measured
            lastNotedWidth[key] = width
            return measured
        }

        func tableView(_ tableView: NSTableView, viewFor column: NSTableColumn?, row: Int) -> NSView? {
            switch rows[row] {
            case .message(let msg):
                return makeMessageCell(message: msg)
            case .boundary(let page):
                return makeBoundaryCell(pageNumber: page)
            }
        }

        // MARK: - Cell construction

        /// Constructs a fresh `NSHostingView` per cell. NSTableView
        /// reuses cell views via `makeView(withIdentifier:)`, but we
        /// don't use that API here because each message has unique
        /// SwiftUI content (different markdown, different layout)
        /// and reuse would require swapping `rootView`, which
        /// invalidates SwiftUI's internal state and defeats the
        /// purpose of reuse. NSTableView's windowing still gives us
        /// the memory win: only visible cells exist; offscreen cells
        /// are released when they scroll out of view.
        ///
        /// Callbacks (`onEditSubmit`, `onRegenerate`) are `nil` for
        /// now. When we wire them, we'll need to stabilize the
        /// closure identity or NSHostingView will re-render every
        /// row on every mutation. Deferred to a later phase.
        private func makeMessageCell(message: Pivox_Ai_V1_Message) -> NSView {
            let hosting = NSHostingView(rootView: Message(
                message: message,
                pinActions: false,
                onEditSubmit: nil,
                onRegenerate: nil))
            return wrapInCell(hosting)
        }

        private func makeBoundaryCell(pageNumber: Int) -> NSView {
            let hosting = NSHostingView(rootView: PageBoundaryRowView(pageNumber: pageNumber))
            return wrapInCell(hosting)
        }

        /// Wraps an `NSHostingView` in an `NSTableCellView` with
        /// four-edge autolayout pinning. NSTableView expects its
        /// cells to be `NSTableCellView` (or a subclass); hosting
        /// the SwiftUI view directly works in some macOS versions
        /// but breaks cell reuse/recycling in others.
        private func wrapInCell(_ hosting: NSView) -> NSView {
            hosting.translatesAutoresizingMaskIntoConstraints = false
            let cell = NSTableCellView()
            cell.addSubview(hosting)
            NSLayoutConstraint.activate([
                hosting.leadingAnchor.constraint(equalTo: cell.leadingAnchor),
                hosting.trailingAnchor.constraint(equalTo: cell.trailingAnchor),
                hosting.topAnchor.constraint(equalTo: cell.topAnchor),
                hosting.bottomAnchor.constraint(equalTo: cell.bottomAnchor),
            ])
            return cell
        }

        // MARK: - Measurement

        private func cacheKey(for row: Row) -> String {
            switch row {
            case .message(let m): return "m:\(m.name)"
            case .boundary(let p): return "b:\(p)"
            }
        }

        /// Current column width, or a best-effort fallback. The
        /// column width is authoritative because rows are laid out
        /// at that width; the scroll view's `contentSize.width` is
        /// only used as a fallback during early initialization
        /// before the column has picked up its autoresized width.
        private func effectiveWidth(tableView: NSTableView? = nil) -> CGFloat {
            let table = tableView ?? self.tableView
            let colWidth = table?.tableColumns.first?.width ?? 0
            if colWidth > 1 { return colWidth }
            return max(scrollView?.contentSize.width ?? 1, 1)
        }

        /// Measures a message's rendered height at the given width.
        ///
        /// Uses `NSHostingController.sizeThatFits(in:)` with an
        /// unbounded height — the SwiftUI view is given the column
        /// width as a hard constraint and returns whatever vertical
        /// space it needs. This is the path that actually works;
        /// `NSHostingView.fittingSize` under-measures rich content
        /// and was the source of the prior attempt's clipping bug.
        ///
        /// Called synchronously on the main thread (must be — SwiftUI
        /// rendering is main-actor-isolated). The reused `sizingHost`
        /// keeps this cheap enough that we can measure 10+ rows per
        /// frame without dropping frames.
        private func measureMessageHeight(_ message: Pivox_Ai_V1_Message, width: CGFloat) -> CGFloat {
            sizingHost.rootView = AnyView(
                Message(
                    message: message,
                    pinActions: false,
                    onEditSubmit: nil,
                    onRegenerate: nil)
                .frame(width: width, alignment: .leading)
            )
            sizingHost.view.layoutSubtreeIfNeeded()
            let fitting = sizingHost.sizeThatFits(in: NSSize(width: width, height: .greatestFiniteMagnitude))
            return max(fitting.height, 1)
        }

        private func measureBoundaryHeight(width: CGFloat) -> CGFloat {
            sizingHost.rootView = AnyView(
                PageBoundaryRowView(pageNumber: 1)
                    .frame(width: width, alignment: .leading)
            )
            sizingHost.view.layoutSubtreeIfNeeded()
            let fitting = sizingHost.sizeThatFits(in: NSSize(width: width, height: .greatestFiniteMagnitude))
            return max(fitting.height, 1)
        }
    }
}

/// Diagnostic page-boundary row, rendered between loaded pages in
/// DEBUG builds so pagination behavior is visually obvious. Not
/// compiled into release builds (see `#if DEBUG` in the coordinator).
private struct PageBoundaryRowView: View {
    let pageNumber: Int

    var body: some View {
        HStack(spacing: 8) {
            Rectangle().fill(Color.red).frame(height: 1)
            Text("Page \(pageNumber)")
                .font(.caption2).bold()
                .foregroundColor(.red)
            Rectangle().fill(Color.red).frame(height: 1)
        }
        .padding(.vertical, 6)
    }
}

private extension NSUserInterfaceItemIdentifier {
    static let messageColumn = NSUserInterfaceItemIdentifier("message")
}
