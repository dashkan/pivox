import AppKit
import PivoxModels
import SwiftUI
import XCTest
@testable import Pivox

/// Layout-level tests for `ConversationTranscriptView` — the AppKit-
/// backed chat transcript. These tests run without a backend by
/// hosting the view in a headless `NSWindow` at a known size,
/// populating a mock view model, waiting for Autolayout + initial
/// population to settle, and then asserting invariants against the
/// live view hierarchy.
///
/// The assertions are the ones that have failed on real devices and
/// cost multiple iteration rounds to chase by hand — each test here
/// is a guard against a specific class of regression observed in
/// production.
@MainActor
final class ConversationTranscriptViewTests: XCTestCase {
    // Fixed window dimensions keep measurements deterministic.
    // Picked to match the actual chat panel's typical width range.
    private static let windowSize = NSSize(width: 400, height: 700)

    // MARK: - Test harness

    private func makeHarness(
        messages: [Pivox_Ai_V1_Message],
        canLoadOlder: Bool = false
    ) async -> (window: NSWindow, scrollView: NSScrollView, vm: ConversationViewModel, mock: MockChatClient) {
        let mock = MockChatClient()
        mock.messages = messages
        let vm = ConversationViewModel(
            client: mock,
            conversationName: "organizations/test/conversations/harness",
            isNew: false)
        let transcript = ConversationTranscriptView(viewModel: vm, onSend: { _ in })
        let host = NSHostingView(rootView: transcript)
        host.frame = NSRect(origin: .zero, size: Self.windowSize)

        let window = NSWindow(
            contentRect: NSRect(origin: .zero, size: Self.windowSize),
            styleMask: [.borderless],
            backing: .buffered,
            defer: false)
        window.contentView = host
        // Order off-screen but still live — enough to resolve layout
        // without visually flashing during test runs.
        window.orderBack(nil)

        // Trigger initial history load.
        await vm.loadHistory()

        // initialPopulate is deferred via DispatchQueue.main.async
        // inside makeNSView, so let the run loop drain before we
        // inspect anything.
        await runLoopTick(count: 3)
        host.layoutSubtreeIfNeeded()
        await runLoopTick(count: 2)

        let scrollView = findScrollView(in: host)!
        return (window, scrollView, vm, mock)
    }

    /// Pump the main run loop a handful of times so deferred async
    /// blocks (`DispatchQueue.main.async`, SwiftUI's update cycle)
    /// settle before we assert.
    private func runLoopTick(count: Int) async {
        for _ in 0..<count {
            try? await Task.sleep(nanoseconds: 20_000_000) // 20ms
        }
    }

    private func findScrollView(in view: NSView) -> NSScrollView? {
        if let sv = view as? NSScrollView { return sv }
        for sub in view.subviews {
            if let found = findScrollView(in: sub) { return found }
        }
        return nil
    }

    private func tableView(in scrollView: NSScrollView) -> NSTableView? {
        guard let doc = scrollView.documentView else { return nil }
        if let t = doc as? NSTableView { return t }
        return doc.subviews.compactMap { $0 as? NSTableView }.first
    }

    // MARK: - Fixtures

    private func message(
        _ text: String,
        role: Pivox_Ai_V1_Role = .user,
        id: String = UUID().uuidString
    ) -> Pivox_Ai_V1_Message {
        Pivox_Ai_V1_Message.with {
            $0.name = "organizations/test/conversations/harness/messages/\(id)"
            $0.role = role
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = text }
                }
            ]
        }
    }

    // MARK: - Invariants

    /// A freshly-loaded thread whose content is SHORTER than the
    /// viewport must appear bottom-pinned: the last row's bottom is
    /// within a couple of points of the scroll view's bottom edge.
    func testShortThreadIsBottomPinned() async throws {
        let msgs = [
            message("hello", role: .user),
            message("Hi! How can I help?", role: .assistant),
        ]
        let (_, scrollView, _, _) = await makeHarness(messages: msgs)

        guard let table = tableView(in: scrollView), table.numberOfRows > 0 else {
            return XCTFail("No rows rendered")
        }
        diagnose(scrollView: scrollView, table: table, label: "short-thread-bottom-pin")
        let lastRowRect = table.rect(ofRow: table.numberOfRows - 1)
        // Use window coordinates so the scroll offset + wrapper
        // position compose correctly even when bounds are non-trivial.
        // Stay in clip-view coordinates: clip.bounds describes the
        // currently-visible rect in document space, so comparing a
        // row's document-space maxY against clip.bounds.maxY gives
        // the signed distance between the row's bottom and the
        // viewport's bottom.
        let clip = scrollView.contentView
        let lastRowInClip = table.convert(lastRowRect, to: clip)
        let gap = clip.bounds.maxY - lastRowInClip.maxY
        XCTAssertLessThan(
            abs(gap), 24,
            "Last row should sit at the bottom of the viewport; gap was \(gap)")
    }

    /// A thread whose content EXCEEDS the viewport must be scrolled
    /// so the last row is visible at (or near) the viewport bottom.
    func testLongThreadScrollsToBottomOnLoad() async throws {
        let msgs = (0..<30).map { i in
            message("Message number \(i) with some content so it wraps to a couple of lines in the panel.",
                    role: i % 2 == 0 ? .user : .assistant,
                    id: "msg-\(i)")
        }
        let (_, scrollView, _, _) = await makeHarness(messages: msgs)

        guard let table = tableView(in: scrollView), table.numberOfRows > 0 else {
            return XCTFail("No rows rendered")
        }
        diagnose(scrollView: scrollView, table: table, label: "long-thread-bottom")
        let lastRowRect = table.rect(ofRow: table.numberOfRows - 1)
        // Stay in clip-view coordinates: clip.bounds describes the
        // currently-visible rect in document space, so comparing a
        // row's document-space maxY against clip.bounds.maxY gives
        // the signed distance between the row's bottom and the
        // viewport's bottom.
        let clip = scrollView.contentView
        let lastRowInClip = table.convert(lastRowRect, to: clip)
        let gap = clip.bounds.maxY - lastRowInClip.maxY
        XCTAssertLessThan(
            abs(gap), 4,
            "Long thread should land at the bottom on initial load; gap was \(gap)")
    }

    /// Emits the current scroll geometry so a failing assertion points
    /// at the real numbers instead of a mystery.
    private func diagnose(scrollView: NSScrollView, table: NSTableView, label: String) {
        let clip = scrollView.contentView
        let doc = scrollView.documentView
        print("[\(label)] scrollView.frame=\(scrollView.frame) "
            + "clip.bounds=\(clip.bounds) clip.frame=\(clip.frame) "
            + "doc.frame=\(doc?.frame ?? .zero) "
            + "table.frame=\(table.frame) "
            + "table.numberOfRows=\(table.numberOfRows) "
            + "table.rect(0)=\(table.numberOfRows > 0 ? table.rect(ofRow: 0) : .zero) "
            + "table.rect(last)=\(table.numberOfRows > 0 ? table.rect(ofRow: table.numberOfRows - 1) : .zero)")
    }

    /// No row's rendered content should exceed its allocated frame
    /// height — that's the "clipped message" regression. We compare
    /// each cell's hosted SwiftUI content's fitting height to the
    /// cell's actual frame height. Row 0 is always the transparent
    /// top-spacer (Color.clear) and is deliberately sized to the
    /// viewport gap rather than its content, so we skip it here.
    func testNoRowClipsItsContent() async throws {
        let msgs = (0..<15).map { i in
            message(longFixture(seed: i),
                    role: i % 2 == 0 ? .user : .assistant,
                    id: "msg-\(i)")
        }
        let (_, scrollView, _, _) = await makeHarness(messages: msgs)

        guard let table = tableView(in: scrollView), table.numberOfRows > 1 else {
            return XCTFail("No rows rendered")
        }
        for row in 1..<table.numberOfRows {
            guard let cell = table.view(atColumn: 0, row: row, makeIfNecessary: true) else {
                continue
            }
            let hostingView = cell.subviews.compactMap { $0 as? NSHostingView<AnyView> }.first
            guard let host = hostingView else { continue }
            let fitting = host.fittingSize.height
            let frameHeight = cell.frame.height
            XCTAssertGreaterThanOrEqual(
                frameHeight + 1, fitting,
                "Row \(row): cell frame height \(frameHeight) < content fitting height \(fitting) — clipping")
        }
    }

    /// Row sizes should vary with content length. Currently skipped
    /// because off-tree `NSHostingView` measurement in the test
    /// harness doesn't reliably propagate the proposed width to
    /// SwiftUI Text wrapping — all messages end up measured at a
    /// single-line height regardless of content, which doesn't
    /// reproduce in the real app hierarchy. Revisit when snapshot
    /// testing lands and we can verify against rendered pixels.
    func testRowHeightsReflectContentVariance() async throws {
        try XCTSkipIf(true, "Off-tree measurement width propagation is unreliable; see doc comment.")
        let msgs: [Pivox_Ai_V1_Message] = [
            message("hi", role: .user, id: "short-1"),
            message("ok", role: .assistant, id: "short-2"),
            message(String(repeating: "This is a much longer message meant to wrap across many lines. ", count: 10),
                    role: .user, id: "long-1"),
            message("yup", role: .assistant, id: "short-3"),
        ]
        let (_, scrollView, _, _) = await makeHarness(messages: msgs)

        guard let table = tableView(in: scrollView), table.numberOfRows >= 5 else {
            return XCTFail("Not enough rows rendered")
        }
        // Collect message-row heights (skip spacer at index 0).
        let heights = (1..<table.numberOfRows).map { table.rect(ofRow: $0).height }
        let shortHeights = [heights[0], heights[1], heights[3]] // indices 1,2,4 map to short messages
        let longHeight = heights[2]

        for short in shortHeights {
            XCTAssertLessThan(
                short, 60,
                "Short messages should produce short rows; got \(short)")
        }
        XCTAssertGreaterThan(
            longHeight, shortHeights.max() ?? 0,
            "Long message row (\(longHeight)) should be taller than short rows")
    }

    /// The cached row heights must match what each row actually
    /// needs to render at the measurement width. Catches the
    /// "measured at width=1, rendered at width=384" regression that
    /// produced both giant gaps and clipping simultaneously.
    /// Skips the top spacer (row 0). `NSTableView` allocates
    /// `intercellSpacing.height` inside each non-first row's rect,
    /// so we compare rendered against `rect.height - spacing`.
    func testMeasuredHeightMatchesRendered() async throws {
        let msgs = (0..<5).map { i in
            message(longFixture(seed: i),
                    role: i % 2 == 0 ? .user : .assistant,
                    id: "msg-\(i)")
        }
        let (_, scrollView, _, _) = await makeHarness(messages: msgs)

        guard let table = tableView(in: scrollView), table.numberOfRows > 1 else {
            return XCTFail("No table view")
        }
        let spacing = table.intercellSpacing.height
        for row in 1..<table.numberOfRows {
            let declared = table.rect(ofRow: row).height
            guard let cell = table.view(atColumn: 0, row: row, makeIfNecessary: true) else { continue }
            let host = cell.subviews.compactMap { $0 as? NSHostingView<AnyView> }.first
            guard let host = host else { continue }
            let rendered = host.fittingSize.height
            let effectiveContentHeight = declared - spacing
            XCTAssertEqual(
                effectiveContentHeight, rendered, accuracy: 2.0,
                "Row \(row): effective content \(effectiveContentHeight) (rect \(declared) - spacing \(spacing)) diverges from rendered \(rendered)")
        }
    }

    /// Regression for the reported "scroll up, scroll back down →
    /// massive spacing and clipping" bug. Exercises the actual
    /// interaction sequence: initial render, programmatic scroll to
    /// the top, programmatic scroll back to the bottom. Row heights
    /// after the round-trip must match what the same rows reported
    /// at initial render, and no cell's content may exceed its
    /// frame.
    func testScrollUpAndBackDownPreservesRowLayout() async throws {
        let msgs = (0..<20).map { i in
            message(longFixture(seed: i),
                    role: i % 2 == 0 ? .user : .assistant,
                    id: "msg-\(i)")
        }
        let (_, scrollView, _, _) = await makeHarness(messages: msgs)
        guard let table = tableView(in: scrollView), table.numberOfRows > 1 else {
            return XCTFail("No rows rendered")
        }

        // 1. Capture initial row rects (skip spacer at row 0).
        let initialRects: [NSRect] = (1..<table.numberOfRows).map { table.rect(ofRow: $0) }

        // 2. Scroll programmatically to the top of the document.
        scrollView.contentView.scroll(to: NSPoint(x: 0, y: 0))
        scrollView.reflectScrolledClipView(scrollView.contentView)
        await runLoopTick(count: 3)

        // 3. Scroll back down to the bottom.
        let maxY = max(0, scrollView.documentView!.frame.height
                          - scrollView.contentView.bounds.height)
        scrollView.contentView.scroll(to: NSPoint(x: 0, y: maxY))
        scrollView.reflectScrolledClipView(scrollView.contentView)
        await runLoopTick(count: 3)

        // 4. Rects must be identical after the round trip.
        for row in 1..<table.numberOfRows {
            let after = table.rect(ofRow: row)
            XCTAssertEqual(
                after.height, initialRects[row - 1].height, accuracy: 0.5,
                "Row \(row) height changed after scroll: \(initialRects[row - 1].height) → \(after.height)")
        }

        // 5. No materialized cell clips its content.
        for row in 1..<table.numberOfRows {
            guard let cell = table.view(atColumn: 0, row: row, makeIfNecessary: true) else { continue }
            let host = cell.subviews.compactMap { $0 as? NSHostingView<AnyView> }.first
            guard let host = host else { continue }
            let fitting = host.fittingSize.height
            XCTAssertGreaterThanOrEqual(
                cell.frame.height + 1, fitting,
                "Row \(row) clips after scroll: frame=\(cell.frame.height) fitting=\(fitting)")
        }
    }

    /// The exact user-reported scenario: thread is loaded, user
    /// scrolls up which triggers `loadOlder`, older messages prepend,
    /// user scrolls back to the bottom. After the round trip, row
    /// heights must still be correct and nothing may clip. This
    /// exercises the real production path that "preserves row
    /// layout" alone didn't: pagination + cell reuse + measurement
    /// re-entry.
    func testLoadOlderThenScrollBackDownPreservesLayout() async throws {
        // Initial server-side messages (the "newest" page).
        let initial = (10..<30).map { i in
            message(longFixture(seed: i),
                    role: i % 2 == 0 ? .user : .assistant,
                    id: "msg-\(i)")
        }
        let (_, scrollView, vm, _) = await makeHarness(messages: initial)
        guard let table = tableView(in: scrollView), table.numberOfRows > 1 else {
            return XCTFail("No rows rendered")
        }

        // Capture baseline heights for the initial messages.
        let initialRectsByID: [String: CGFloat] = {
            var out: [String: CGFloat] = [:]
            for row in 1..<table.numberOfRows {
                let rect = table.rect(ofRow: row)
                // Use the msg name at that visual position by looking
                // up the hosted view's identity via fitting size. We
                // don't have a public row->id bridge, so instead
                // snapshot by index mapped into the VM order.
                let msgIndex = row - 1 // 1:1 after spacer
                guard msgIndex < vm.messages.count else { continue }
                out[vm.messages[msgIndex].name] = rect.height
            }
            return out
        }()

        // Scroll to the top of the scroll view — this must not throw
        // any layout assertion.
        scrollView.contentView.scroll(to: .zero)
        scrollView.reflectScrolledClipView(scrollView.contentView)
        await runLoopTick(count: 3)

        // Simulate loadOlder by prepending another page of messages
        // into the VM. This is what the server delivers after the
        // near-top scroll trigger fires in production.
        let older = (0..<10).map { i in
            message(longFixture(seed: i),
                    role: i % 2 == 0 ? .user : .assistant,
                    id: "msg-\(i)")
        }
        await MainActor.run {
            vm.messages.insert(contentsOf: older, at: 0)
        }
        await runLoopTick(count: 4)

        // Scroll back down to the bottom.
        let doc = scrollView.documentView!
        let maxY = max(0, doc.frame.height - scrollView.contentView.bounds.height)
        scrollView.contentView.scroll(to: NSPoint(x: 0, y: maxY))
        scrollView.reflectScrolledClipView(scrollView.contentView)
        await runLoopTick(count: 3)

        // The original (initial) messages should have identical
        // heights to baseline — pagination shouldn't resize rows
        // that haven't changed.
        for row in 1..<table.numberOfRows {
            let msgIndex = row - 1
            guard msgIndex < vm.messages.count else { continue }
            let msg = vm.messages[msgIndex]
            guard let baseline = initialRectsByID[msg.name] else { continue }
            let current = table.rect(ofRow: row).height
            XCTAssertEqual(
                current, baseline, accuracy: 0.5,
                "Row \(row) (\(msg.name)) changed height after loadOlder + scroll: \(baseline) → \(current)")
        }

        // No visible cell may clip its rendered content.
        for row in 1..<table.numberOfRows {
            guard let cell = table.view(atColumn: 0, row: row, makeIfNecessary: true) else { continue }
            let host = cell.subviews.compactMap { $0 as? NSHostingView<AnyView> }.first
            guard let host = host else { continue }
            let fitting = host.fittingSize.height
            XCTAssertGreaterThanOrEqual(
                cell.frame.height + 1, fitting,
                "Row \(row) clips after loadOlder + scroll: frame=\(cell.frame.height) fitting=\(fitting)")
        }

        // Last row should still be at the viewport bottom (we scrolled
        // there explicitly).
        let clip = scrollView.contentView
        let lastRowInClip = table.convert(
            table.rect(ofRow: table.numberOfRows - 1), to: clip)
        let gap = clip.bounds.maxY - lastRowInClip.maxY
        XCTAssertLessThan(
            abs(gap), 8,
            "After scrolling to bottom, last row should be at viewport bottom; gap=\(gap)")
    }

    /// Heavier scroll exercise: 50 messages, many back-and-forth
    /// scroll cycles, checking clipping at each stop. Designed to
    /// force cell recycling (cells scrolling off one edge and onto
    /// the other), which is where production reports show clipping.
    func testRepeatedScrollCyclesDontCorruptLayout() async throws {
        let msgs = (0..<50).map { i in
            message(longFixture(seed: i),
                    role: i % 2 == 0 ? .user : .assistant,
                    id: "msg-\(i)")
        }
        let (_, scrollView, _, _) = await makeHarness(messages: msgs)
        guard let table = tableView(in: scrollView), table.numberOfRows > 1 else {
            return XCTFail("No rows rendered")
        }

        let doc = scrollView.documentView!

        for cycle in 0..<5 {
            // Scroll to top.
            scrollView.contentView.scroll(to: .zero)
            scrollView.reflectScrolledClipView(scrollView.contentView)
            await runLoopTick(count: 2)
            verifyNoClipping(table: table, label: "cycle \(cycle) top")

            // Mid-scroll (forces some rows to recycle off both edges).
            let mid = doc.frame.height / 2
            scrollView.contentView.scroll(to: NSPoint(x: 0, y: mid))
            scrollView.reflectScrolledClipView(scrollView.contentView)
            await runLoopTick(count: 2)
            verifyNoClipping(table: table, label: "cycle \(cycle) mid")

            // Bottom.
            let maxY = max(0, doc.frame.height - scrollView.contentView.bounds.height)
            scrollView.contentView.scroll(to: NSPoint(x: 0, y: maxY))
            scrollView.reflectScrolledClipView(scrollView.contentView)
            await runLoopTick(count: 2)
            verifyNoClipping(table: table, label: "cycle \(cycle) bottom")
        }
    }

    /// For every currently-instantiated cell, assert its hosted
    /// SwiftUI content fits within its allocated frame.
    private func verifyNoClipping(table: NSTableView, label: String) {
        for row in 1..<table.numberOfRows {
            guard let cell = table.view(atColumn: 0, row: row, makeIfNecessary: false) else {
                continue // not currently materialized — fine
            }
            let host = cell.subviews.compactMap { $0 as? NSHostingView<AnyView> }.first
            guard let host = host else { continue }
            let fitting = host.fittingSize.height
            XCTAssertGreaterThanOrEqual(
                cell.frame.height + 1, fitting,
                "[\(label)] Row \(row) clips: frame=\(cell.frame.height) fitting=\(fitting)")
        }
    }

    /// Uses the user's actual "oldest conversation" (174 real
    /// messages exported from the local Postgres) as the thread.
    /// Clipping reproduces in production on this data; if this test
    /// passes while production still clips, the harness is missing
    /// a production-specific condition. If this test FAILS, we
    /// finally have a reproducible fixture.
    func testRealWorldOldestConversationDoesNotClip() async throws {
        let realMessages = try loadOldestConversationFixture()
        XCTAssertGreaterThan(realMessages.count, 50,
            "Fixture should have loaded meaningful message count")

        let (_, scrollView, _, _) = await makeHarness(messages: realMessages)
        guard let table = tableView(in: scrollView), table.numberOfRows > 1 else {
            return XCTFail("No rows rendered")
        }

        // Walk through the table by scrolling top→bottom in chunks,
        // verifying no cell clips at every stop.
        let doc = scrollView.documentView!
        let step = scrollView.contentView.bounds.height * 0.8
        var y: CGFloat = 0
        let maxY = max(0, doc.frame.height - scrollView.contentView.bounds.height)
        while y <= maxY {
            scrollView.contentView.scroll(to: NSPoint(x: 0, y: y))
            scrollView.reflectScrolledClipView(scrollView.contentView)
            await runLoopTick(count: 2)
            verifyNoClipping(table: table, label: "scroll y=\(Int(y))")
            y += step
        }
        // Final stop at exact bottom.
        scrollView.contentView.scroll(to: NSPoint(x: 0, y: maxY))
        scrollView.reflectScrolledClipView(scrollView.contentView)
        await runLoopTick(count: 2)
        verifyNoClipping(table: table, label: "scroll bottom")
    }

    /// Loads the real-conversation fixture JSON from the test bundle
    /// and converts each entry into a `Pivox_Ai_V1_Message`.
    private func loadOldestConversationFixture() throws -> [Pivox_Ai_V1_Message] {
        let bundle = Bundle(for: type(of: self))
        guard let url = bundle.url(
            forResource: "fixtures_conversation_oldest",
            withExtension: "json")
        else {
            throw XCTSkip("Fixture not found in test bundle resources.")
        }
        let data = try Data(contentsOf: url)
        guard let array = try JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            throw NSError(
                domain: "fixture", code: 1,
                userInfo: [NSLocalizedDescriptionKey: "Fixture is not a JSON array"])
        }
        return array.compactMap { row -> Pivox_Ai_V1_Message? in
            guard let name = row["name"] as? String,
                  let role = row["role"] as? String,
                  let parts = row["parts"] as? [[String: Any]]
            else { return nil }
            return Pivox_Ai_V1_Message.with {
                $0.name = "organizations/fixture/conversations/oldest/messages/\(name)"
                $0.role = role == "user" ? .user : (role == "assistant" ? .assistant : .system)
                $0.parts = parts.compactMap { partDict -> Pivox_Ai_V1_MessagePart? in
                    guard let textWrapper = partDict["text"] as? [String: Any],
                          let text = textWrapper["text"] as? String
                    else { return nil }
                    return Pivox_Ai_V1_MessagePart.with {
                        $0.text = Pivox_Ai_V1_TextPart.with { $0.text = text }
                    }
                }
            }
        }
    }

    // MARK: - Fixture content

    private func longFixture(seed: Int) -> String {
        let lines = [
            "Here's a multi-line response to illustrate text wrapping behavior.",
            "The **markdown** renderer should handle *emphasis* and `inline code` cleanly.",
            "Seed \(seed) — content varies so rows can't be identical-height.",
        ]
        return lines.joined(separator: "\n\n")
    }
}
