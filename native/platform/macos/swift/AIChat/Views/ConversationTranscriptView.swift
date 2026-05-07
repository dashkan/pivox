import AppKit
import Combine
import PivoxModels
import SwiftUI

// NSEvent isn't declared `Sendable` by AppKit, but Swift 6 requires
// the handler passed to `addLocalMonitorForEvents` to be `@Sendable`,
// which in turn requires its parameter type to conform. The monitor
// only fires on the main thread for UI events, so the value is
// effectively main-actor-isolated — declaring unchecked Sendable is
// the same assumption the rest of AppKit already makes internally.
extension NSEvent: @retroactive @unchecked Sendable {}

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
/// Edit/regenerate callbacks are deliberately NOT in this file yet.
/// They layer in as subsequent phases with their own gates.
///
/// Bottom-pin-when-short: handled by `BottomPinClipView` (below),
/// which overrides `constrainBoundsRect` to force a negative
/// `bounds.origin.y` when the document is shorter than the
/// viewport. The viewport then "looks at" coordinates above the
/// document, naturally placing the rows flush at the bottom of the
/// visible area. NSScrollView's normal scroll-clamping resumes the
/// moment content outgrows the viewport (the override no-ops).
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
        scrollView.borderType = .noBorder
        // Transparent background at BOTH layers: the scroll view
        // itself (which is what `drawsBackground = false` controls)
        // AND the content view (NSClipView, which independently draws
        // `NSColor.controlBackgroundColor` by default → near-black in
        // dark mode, visible as a dark edge against the parent panel).
        scrollView.drawsBackground = false
        scrollView.backgroundColor = .clear

        // Swap the default clip view for our bottom-pinning subclass
        // BEFORE attaching the document view, so the documentView
        // setter wires the table to our custom clip view from the
        // start. Inherits transparent drawing.
        let clipView = BottomPinClipView()
        clipView.drawsBackground = false
        scrollView.contentView = clipView

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

        // Document view frame changes → docHeight changed →
        // bottom-pin needs re-application. `constrainBoundsRect` is
        // a passive filter; we have to actively poke the clip view
        // by re-writing its bounds via `bounds = constrainBoundsRect(bounds)`.
        // See `tableViewFrameDidChange` below for the trigger.
        tableView.postsFrameChangedNotifications = true
        NotificationCenter.default.addObserver(
            context.coordinator,
            selector: #selector(Coordinator.tableViewFrameDidChange(_:)),
            name: NSView.frameDidChangeNotification,
            object: tableView)

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

        // Jump-to-latest pill click handler. Pill visibility itself
        // is driven off `contentViewBoundsDidChange` below — purely
        // a function of how far from the document bottom the user
        // currently is, with no gesture-tracking state. That avoids
        // the flicker where a downward-scroll-at-bottom would
        // momentarily flip a "user is interacting" flag and reveal
        // the pill for one frame, even though the user never
        // actually left the bottom.
        NotificationCenter.default.addObserver(
            context.coordinator,
            selector: #selector(Coordinator.handleJumpToLatest(_:)),
            name: .aiChatJumpToLatest,
            object: nil)
        NotificationCenter.default.addObserver(
            context.coordinator,
            selector: #selector(Coordinator.handleScrollUp(_:)),
            name: .aiChatScrollUp,
            object: nil)

        // Install the scroll-event forwarder so vertical scrolls over
        // nested horizontal scroll views (code blocks) reach our
        // transcript scroll view instead of being swallowed by the
        // inner scroller. See `installScrollMonitor` for details.
        context.coordinator.installScrollMonitor()

        return scrollView
    }

    static func dismantleNSView(_ nsView: NSScrollView, coordinator: Coordinator) {
        coordinator.teardown()
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

        /// Latch — set the first time we observe the document being
        /// at least as tall as the clip view. Once set, `applyBottomPin`
        /// short-circuits forever. The latch defends against the
        /// scroll-stutter regression: width-aware height refresh
        /// during the first scroll-up invalidates rows tick-by-tick,
        /// briefly flickering `tableView.frame` to a smaller value
        /// before relayout settles, and `tableView.rect(ofRow: last)`
        /// can return stale-but-nonzero numbers in that window. The
        /// `maxY > 0` guard skips the all-zero case but not the
        /// stale-nonzero case. We don't legitimately need to
        /// re-engage the pin once content has ever filled the
        /// viewport — messages are append-only, conversation
        /// switches re-mount the coordinator from scratch (via
        /// `.id(name)` on `ActiveConversationView`), so the latch
        /// resets where it should.
        private var docHasEverOverflowed = false

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

        /// Window-level scroll-wheel monitor used to forward
        /// predominantly vertical scroll events to our scroll view
        /// even when the cursor is over a nested horizontal-scroll
        /// view (e.g. a code block). Without this, hit-testing
        /// dispatches the event to the inner `NSScrollView`, which
        /// swallows it even though it can only scroll horizontally —
        /// producing the "mouse over code block freezes transcript
        /// scroll" bug.
        private var scrollMonitor: Any?

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

        // MARK: - Scroll forwarding

        func installScrollMonitor() {
            scrollMonitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] event in
                guard let self else { return event }
                return MainActor.assumeIsolated { self.routeScrollEvent(event) }
            }
        }

        func teardown() {
            if let monitor = scrollMonitor {
                NSEvent.removeMonitor(monitor)
                scrollMonitor = nil
            }
        }

        /// Scrolls the clip view origin to the bottom of the
        /// document. Reads `tableView.frame.height` after a forced
        /// layout pass — once `heightOfRow` is honest about
        /// pin-actions content (see `measureMessageHeight`'s
        /// `pinActions` parameter and `cacheKey`'s pin keying),
        /// NSTableView's tile produces a frame height that includes
        /// the last assistant's icon row.
        ///
        /// The earlier symptom — last response cut off behind the
        /// input box — looked like a stale-frame.height bug but was
        /// actually the height-cache lying: measurement passed
        /// `pinActions: false` while rendering used `true`, so the
        /// last row's measured height excluded the icon row's space.
        /// `frame.height` was right per its inputs; the inputs were
        /// wrong. Fixing measurement is the load-bearing change;
        /// this function is just a thin wrapper around the now-honest
        /// frame.
        private func scrollToBottom() {
            guard let scrollView, let tableView, !rows.isEmpty else { return }
            tableView.layoutSubtreeIfNeeded()
            let docHeight = tableView.frame.height
            let clipHeight = scrollView.contentView.bounds.height
            guard docHeight > clipHeight else { return }
            let origin = NSPoint(x: 0, y: docHeight - clipHeight)
            scrollView.contentView.scroll(to: origin)
            scrollView.reflectScrolledClipView(scrollView.contentView)
        }

        /// Routes a scroll wheel event: if it's inside our scroll
        /// view and predominantly vertical, we deliver it directly
        /// to our scroll view and consume it (return nil). If it's
        /// predominantly horizontal, we let it continue down the
        /// normal dispatch path so code blocks' inner horizontal
        /// scrolling still works. Events outside our scroll view or
        /// from other windows pass through unchanged.
        private func routeScrollEvent(_ event: NSEvent) -> NSEvent? {
            guard let scrollView, let window = scrollView.window,
                  event.window === window else { return event }
            let pointInScroll = scrollView.convert(event.locationInWindow, from: nil)
            guard scrollView.bounds.contains(pointInScroll) else { return event }
            if abs(event.scrollingDeltaY) >= abs(event.scrollingDeltaX) {
                scrollView.scrollWheel(with: event)
                return nil
            }
            return event
        }

        // MARK: - Sync

        func sync(viewModel: ConversationViewModel) {
            // VM identity change → fresh conversation in this
            // coordinator. Reset the bottom-pin overflow latch so
            // a short conversation can pin again. Today this only
            // fires if `.id(name)` is removed from
            // `ActiveConversationView` and the same coordinator is
            // reused across conversations — defensive against a
            // future tree restructure that would silently break
            // bottom-pin without this guard.
            if self.viewModel !== viewModel {
                docHasEverOverflowed = false
            }
            self.viewModel = viewModel
            let new = viewModel.messages

            // Streaming text update: same count, same names, but
            // the last message's text grew. The placeholder
            // assistant message is being filled in delta by delta.
            // The height cache keys by message name, so a naive
            // reload here would render at the cached (small) height
            // from the first delta. Invalidate the cache entry for
            // the streaming row, then `noteHeightOfRows` so
            // NSTableView re-asks `heightOfRow` with the new text.
            if let lastNew = new.last, let lastOld = messages.last,
               new.count == messages.count,
               lastNew.name == lastOld.name,
               Self.textOf(lastNew) != Self.textOf(lastOld) {
                // Streaming delta: the placeholder's text grew. Gate
                // auto-scroll on `viewModel.stickToBottom` (user-intent
                // flag), NOT a position-based `isAtBottom()` read.
                // Reason: each delta marks heights dirty via
                // `noteHeightOfRows` but doesn't synchronously commit
                // layout. Multiple deltas queue inside a single
                // runloop tick — by delta 2, `visible.maxY` is still
                // pre-delta-1 while `docHeight` reflects delta-1's
                // pending growth, so a position check reads "user
                // moved away" and aborts the scroll for every delta
                // after the first. Result: viewport falls progressively
                // behind the streaming cursor, exactly the symptom
                // observed in the chat-not-scrolling-during-stream
                // bug.
                //
                // `stickToBottom` is set true on send and only flips
                // false when the user actively scrolls past the
                // one-viewport pill threshold (see updatePillVisibility),
                // so it correctly survives cross-delta layout flicker.
                // Same gate the tail-append branch below uses.
                messages = new
                rebuildRowsKeepingPages()
                if let lastRowIdx = rows.indices.last {
                    // Streaming row IS the last assistant by
                    // construction (it's the placeholder being
                    // filled). Invalidate the pinned key so the
                    // next heightOfRow re-measures with the icons
                    // included.
                    let pinned = isPinnedActionsRow(at: lastRowIdx)
                    let key = cacheKey(for: rows[lastRowIdx], pinned: pinned)
                    heightCache.removeValue(forKey: key)
                    lastNotedWidth.removeValue(forKey: key)
                    let last = IndexSet(integer: lastRowIdx)
                    tableView?.noteHeightOfRows(withIndexesChanged: last)
                    tableView?.reloadData(forRowIndexes: last,
                                          columnIndexes: IndexSet(integer: 0))
                }
                if viewModel.stickToBottom {
                    DispatchQueue.main.async { [weak self] in
                        self?.scrollToBottom()
                    }
                } else {
                    // User has scrolled away. Doc grew but clip
                    // origin didn't move, so the bounds-change
                    // observer won't fire — recheck pill visibility
                    // explicitly so the distance-from-bottom can
                    // cross the pill threshold.
                    DispatchQueue.main.async { [weak self] in
                        self?.updatePillVisibility()
                    }
                }
                return
            }

            applyMessages(new)
        }

        /// Concatenated plain text of all `text` parts in a message.
        /// Used to detect streaming-text mutations that
        /// `messagesUnchanged` (which compares by name) would
        /// otherwise miss.
        private static func textOf(_ msg: Pivox_Ai_V1_Message) -> String {
            msg.parts.compactMap { p -> String? in
                if case .text(let tp) = p.part { return tp.text }
                return nil
            }.joined()
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
                    // before row rects are valid. Calling scroll
                    // synchronously lands on stale (pre-layout)
                    // coordinates — you end up somewhere mid-content
                    // instead of the bottom.
                    //
                    // We do NOT use `scrollRowToVisible(last)` here:
                    // that API only scrolls the minimum amount to
                    // make the row visible, which leaves the last
                    // row flush against the top of the viewport if
                    // content is tall. We want the transcript bottom
                    // flush with the viewport bottom — newest
                    // message sitting just above the prompt input —
                    // so we scroll the clip view origin explicitly
                    // to `contentHeight - clipHeight`.
                    DispatchQueue.main.async { [weak self] in
                        self?.scrollToBottom()
                        // Bottom-pin for short conversations:
                        // `scrollToBottom` early-returns when
                        // docHeight ≤ clipHeight, so we still need
                        // to apply the negative-origin pin
                        // explicitly. `applyBottomPin` no-ops when
                        // content fills the viewport, so calling it
                        // unconditionally is safe.
                        self?.applyBottomPin()
                    }
                }
            } else if prependCount > 0 {
                // Older page prepended. Preserve scroll.
                applyPrepend(newMessages: new, prependCount: prependCount)
            } else if newCount > oldCount {
                // Tail append: user just sent a message, or a
                // streamed assistant response just committed. The
                // user's mental model is "the new message I asked
                // for is at the bottom" — keep the viewport pinned
                // there. We gate on `viewModel.stickToBottom` (the
                // pill-visibility flag) rather than `isAtBottom()`
                // because `ConversationViewModel.send()` forces
                // `stickToBottom = true` to express intent: "the
                // user just hit Send, take them to the new message
                // even if they were scrolled up reading earlier
                // history." A pure position check would ignore that
                // intent.
                messages = new
                rebuildRowsKeepingPages()
                tableView?.reloadData()
                if viewModel.stickToBottom {
                    DispatchQueue.main.async { [weak self] in
                        self?.scrollToBottom()
                    }
                } else {
                    DispatchQueue.main.async { [weak self] in
                        self?.updatePillVisibility()
                    }
                }
            } else if newCount != oldCount {
                // Other size changes (message edit, regenerate
                // later) — structural reload, no scroll change.
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
                // Prepended rows are older than every existing row,
                // so they're never the last assistant — pinned=false.
                let h = measureMessageHeight(msg, pinActions: false, width: width)
                let key = cacheKey(for: .message(msg), pinned: false)
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
            let boundaryKey = cacheKey(for: .boundary(page: newPageNumber))
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
            if abs(newWidth - cachedWidth) > 0.5 {
                cachedWidth = newWidth
                refreshVisibleRowHeights()
            }
            // Vertical resize changes clipHeight → re-apply bottom-pin.
            applyBottomPin()
        }

        /// Document view frame changes (rows added, removed, height
        /// invalidated). docHeight just changed; force-apply the
        /// bottom-pin so the clip view's bounds re-evaluate against
        /// the new height.
        @objc func tableViewFrameDidChange(_ notification: Notification) {
            applyBottomPin()
        }

        /// Bottom-pin-when-short: when the document is shorter than
        /// the viewport, we write the clip view's bounds origin to
        /// a NEGATIVE Y value so the viewport "looks at" coordinates
        /// above the document. The document — anchored at y=0 in
        /// flipped coordinates — then renders flush against the
        /// bottom of the visible area, with empty space above.
        ///
        /// Why we don't rely on `BottomPinClipView.constrainBoundsRect`
        /// alone: the override is a passive filter; AppKit only
        /// invokes it when an active scroll proposal needs
        /// validation. Initial layout and `reloadData`-driven docHeight
        /// changes don't trigger that path, so we have to write the
        /// bounds directly. We use `bounds = ...` (not `scroll(to:)`)
        /// to skip AppKit's animation interpolation during streaming
        /// row growth and live-resize.
        ///
        /// When content overflows the viewport (`docHeight >=
        /// clipHeight`), we restore origin.y to 0 if it's currently
        /// negative — otherwise the negative origin from a previous
        /// short-content state would persist past the threshold.
        private func applyBottomPin() {
            // Once the document has ever exceeded the clip view's
            // height, the pin is permanently disengaged for this
            // coordinator's lifetime. See `docHasEverOverflowed`
            // for why this latch exists.
            if docHasEverOverflowed { return }
            guard let scrollView, let tableView else { return }
            // Layout-in-progress guard. `tableView.rect(ofRow:)`
            // returns NSZeroRect for rows that haven't been laid
            // out yet, which happens transiently during the
            // noteHeightOfRows / reloadData passes that scroll-time
            // width-aware height refresh triggers. Without this
            // guard, the table-frame notification that fires
            // mid-relayout would read docHeight as 0, decide
            // content is "short", and snap origin.y to `-clipHeight`.
            // A follow-up frame-change notification fires after
            // layout settles and we apply the pin then.
            let rowCount = tableView.numberOfRows
            guard rowCount > 0 else { return }
            let lastRect = tableView.rect(ofRow: rowCount - 1)
            guard lastRect.maxY > 0 else { return }

            let clipView = scrollView.contentView
            // True content extent (bottom of last row in document
            // coords). We deliberately don't use
            // `documentView.frame.height` — NSTableView pads its
            // own frame to the clip height when content is short,
            // so frame.height always reads >= clipHeight and the
            // bottom-pin check would never trigger.
            let docHeight = lastRect.maxY
            let clipHeight = clipView.bounds.height

            // Latch first. Even on a "no-op" overflow path (origin
            // already 0, content fills viewport), set the flag so
            // future calls during scroll-driven height refresh skip
            // the function entirely.
            if docHeight >= clipHeight {
                docHasEverOverflowed = true
            }

            var newBounds = clipView.bounds
            if docHeight < clipHeight {
                newBounds.origin.y = docHeight - clipHeight
            } else if clipView.bounds.origin.y < 0 {
                newBounds.origin.y = 0
            } else {
                return
            }
            if newBounds != clipView.bounds {
                clipView.bounds = newBounds
                scrollView.reflectScrolledClipView(clipView)
            }
        }

        /// Jump-to-latest pill clicked. Force re-engage stickiness
        /// and snap to bottom; subsequent streaming deltas / tail
        /// appends will follow as they arrive.
        @objc func handleJumpToLatest(_ notification: Notification) {
            viewModel.stickToBottom = true
            scrollToBottom()
        }

        /// ⌘↑ keyboard shortcut. Single decisive action: scroll to
        /// the top of currently-loaded content. The existing
        /// scroll-position observer in `contentViewBoundsDidChange`
        /// notices `origin.y < 300` and fires one `loadOlder()`
        /// (gated by `expectingPrepend`, so no cascading). After
        /// the prepend's scroll-preservation shifts the viewport
        /// down to the boundary between old and new pages, the
        /// next ⌘↑ press starts the cycle again — one press per
        /// page traversal. When no more pages remain, `loadOlder`
        /// gates itself off via `canLoadOlder`, and ⌘↑ just sits
        /// at the absolute top.
        ///
        /// We deliberately don't fire `loadOlder()` directly here.
        /// The bounds-change observer's path is already correct;
        /// re-implementing it would mean two places to keep in
        /// sync with the prepend / latch logic.
        @objc func handleScrollUp(_ notification: Notification) {
            guard let scrollView else { return }
            let clipView = scrollView.contentView
            if clipView.bounds.origin.y > 0 {
                clipView.scroll(to: .zero)
                scrollView.reflectScrolledClipView(clipView)
            }
        }

        /// Whether the visible rect's bottom is essentially at the
        /// document's bottom edge. Tight tolerance — used to decide
        /// whether to auto-scroll on streaming deltas / tail
        /// appends. Anything further than 30pt from the bottom
        /// counts as "user has moved away" and we leave them alone.
        private func isAtBottom() -> Bool {
            guard let scrollView else { return true }
            let visible = scrollView.contentView.documentVisibleRect
            let docHeight = scrollView.documentView?.frame.height ?? 0
            let tolerance: CGFloat = 30
            return visible.maxY >= docHeight - tolerance
        }

        /// Recomputes `viewModel.stickToBottom` from current scroll
        /// position. The flag drives the jump-to-latest pill, NOT
        /// auto-scroll behavior — see `isAtBottom()` for the tight
        /// auto-scroll gate.
        ///
        /// Threshold is one viewport height: the pill only appears
        /// once the user has scrolled up far enough that the
        /// previously-visible "page" of content (everything that was
        /// on screen) is fully off-screen. Smaller scrolls — incidental
        /// upward nudges, downward scrolls at the bottom, momentum
        /// tails — never cross the threshold, so the pill doesn't
        /// flicker during gestures. By the time the pill shows up,
        /// the user has unambiguously navigated away and the affordance
        /// is genuinely useful.
        private func updatePillVisibility() {
            guard let scrollView else { return }
            let visible = scrollView.contentView.documentVisibleRect
            let docHeight = scrollView.documentView?.frame.height ?? 0
            let viewportHeight = scrollView.contentView.bounds.height
            let threshold = max(120, viewportHeight)
            let nearBottom = visible.maxY >= docHeight - threshold
            if viewModel.stickToBottom != nearBottom {
                viewModel.stickToBottom = nearBottom
            }
        }

        @objc func contentViewBoundsDidChange(_ notification: Notification) {
            // Every scroll tick: catch any rows newly scrolled into
            // view whose heights were measured at an older width.
            // This is the companion to the resize handler's "only
            // refresh visible" — rows not visible during a resize
            // get refreshed here as they scroll into view.
            refreshVisibleRowHeights()

            // Pill visibility tracks the user's actual scroll
            // position continuously: it only appears once they've
            // scrolled away by more than one viewport. This is how
            // we get "no flicker on downward scrolls at the bottom"
            // and "pill appears as soon as it's actually useful"
            // without any gesture-tracking state.
            updatePillVisibility()

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
                let key = cacheKey(for: rows[row], pinned: isPinnedActionsRow(at: row))
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
            let pinned = isPinnedActionsRow(at: row)
            let key = cacheKey(for: rows[row], pinned: pinned)
            if lastNotedWidth[key] == width, let cached = heightCache[key] {
                return cached
            }
            let measured: CGFloat
            switch rows[row] {
            case .message(let msg):
                measured = measureMessageHeight(msg, pinActions: pinned, width: width)
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
                return makeMessageCell(message: msg, pinActions: msg.name == lastAssistantName)
            case .boundary(let page):
                return makeBoundaryCell(pageNumber: page)
            }
        }

        /// Resource name of the most recent assistant message in the
        /// transcript, or empty if there isn't one. Used to pin the
        /// action row on that message so the user always has copy /
        /// regenerate / feedback affordances on the latest reply
        /// without hovering — Gemini-style. Earlier assistant turns
        /// keep the hover-reveal behavior to stay visually quiet.
        private var lastAssistantName: String {
            for row in rows.reversed() {
                if case .message(let m) = row, m.role == .assistant {
                    return m.name
                }
            }
            return ""
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
        private func makeMessageCell(message: Pivox_Ai_V1_Message, pinActions: Bool) -> NSView {
            let hosting = NSHostingView(rootView: Message(
                message: message,
                pinActions: pinActions,
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

        /// Cache key for a row. For messages, the `pinned` flag is
        /// part of the key because the rendered cell's height depends
        /// on it: the most-recent assistant message renders with a
        /// pinned action row (thumbs/copy icons) that adds visible
        /// vertical space. Same message resource at different pin
        /// states is legitimately a different row height — caching
        /// by name alone caches the WRONG height for whichever state
        /// wasn't measured first.
        ///
        /// Boundaries don't depend on pin state; the parameter is
        /// ignored for them.
        private func cacheKey(for row: Row, pinned: Bool = false) -> String {
            switch row {
            case .message(let m): return "m:\(m.name):p=\(pinned ? 1 : 0)"
            case .boundary(let p): return "b:\(p)"
            }
        }

        /// Whether the row at `index` should render with pinned
        /// actions (the action-row icons that appear under the most
        /// recent assistant message). Mirrors the pin selection
        /// logic in `tableView(_:viewFor:row:)` so measurement and
        /// rendering stay in lockstep.
        private func isPinnedActionsRow(at index: Int) -> Bool {
            guard index >= 0, index < rows.count else { return false }
            if case .message(let m) = rows[index],
               m.role == .assistant,
               m.name == lastAssistantName {
                return true
            }
            return false
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
        private func measureMessageHeight(_ message: Pivox_Ai_V1_Message, pinActions: Bool, width: CGFloat) -> CGFloat {
            sizingHost.rootView = AnyView(
                Message(
                    message: message,
                    pinActions: pinActions,
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

/// Clip view that bottom-pins short content.
///
/// `NSClipView.constrainBoundsRect(_:)` is AppKit's hook for
/// validating any proposed scroll position. The default
/// implementation clamps `origin.y` to `[0, max(0, docHeight -
/// clipHeight)]`, which collapses to just `0` when content fits in
/// the viewport — leaving the document top-aligned with empty space
/// below. We override to force `origin.y = docHeight - clipHeight`
/// (NEGATIVE) when the document is shorter than the viewport: the
/// clip view then "looks at" a region above the document, and the
/// document renders flush against the bottom of the visible area.
/// Same effect `.defaultScrollAnchor(.bottom)` gave us in the
/// pre-AppKit SwiftUI version.
///
/// # Why the coordinator drives application
///
/// `constrainBoundsRect` is a passive filter — only called when a
/// scroll change is actively proposed. On initial layout and during
/// data mutations, the clip view's origin stays at `(0,0)`, AppKit
/// never asks the override to weigh in, and the bottom-pin never
/// applies. Forcing it requires an explicit
/// `clipView.bounds = clipView.constrainBoundsRect(clipView.bounds)`
/// after the layout settles. The coordinator's frame-change
/// observers are the right hook — overriding the clip view's
/// `frame` / `documentView` properties to self-observe doesn't
/// reliably fire when AppKit sets those via Obj-C internals.
///
/// The override is a no-op once content outgrows the viewport
/// (`docHeight >= clipHeight`); `super`'s default clamping resumes
/// and normal scrolling works.
private final class BottomPinClipView: NSClipView {
    override func constrainBoundsRect(_ proposedBounds: NSRect) -> NSRect {
        var rect = super.constrainBoundsRect(proposedBounds)
        guard let table = documentView as? NSTableView else { return rect }

        // True content extent — `frame.height` lies (NSTableView
        // pads its frame to clip height when content is short).
        let rowCount = table.numberOfRows
        guard rowCount > 0 else { return rect }
        let docHeight = table.rect(ofRow: rowCount - 1).maxY
        let clipHeight = proposedBounds.height

        if docHeight < clipHeight {
            rect.origin.y = docHeight - clipHeight
        }
        return rect
    }
}
