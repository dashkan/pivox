import XCTest
@testable import Pivox

/// Round-trip persistence tests for `AIChatState`.
///
/// We construct each instance against a sandboxed `UserDefaults`
/// suite so the tests don't pollute the real defaults database
/// and don't fight a parallel test that might read the same
/// values. Each test cleans up its suite via
/// `removePersistentDomain` in a `defer` block.
class AIChatStateTests: XCTestCase {

  // MARK: - Layout mode

  @MainActor
  func testLayoutModeDefaultsToFloat() {
    // Fresh defaults — never written to. New `AIChatState` should
    // pick the default layout, not nil out or crash.
    let (defaults, suiteName) = isolatedDefaults()
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let state = AIChatState(defaults: defaults)
    XCTAssertEqual(state.layoutMode, .float,
                   "Float is the documented default layout — flipping it " +
                   "is a UX policy change worth catching here.")
  }

  @MainActor
  func testLayoutModeRoundTripsThroughDefaults() {
    let (defaults, suiteName) = isolatedDefaults()
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let writer = AIChatState(defaults: defaults)
    writer.layoutMode = .push

    let reader = AIChatState(defaults: defaults)
    XCTAssertEqual(reader.layoutMode, .push,
                   "Setting layoutMode on one AIChatState instance must " +
                   "be visible to the next one constructed against the " +
                   "same defaults — that's how the setting survives launches.")
  }

  // MARK: - Mode (docked / detached)

  @MainActor
  func testModeDefaultsToDocked() {
    let (defaults, suiteName) = isolatedDefaults()
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let state = AIChatState(defaults: defaults)
    XCTAssertEqual(state.mode, .docked)
  }

  @MainActor
  func testModeRoundTripsThroughDefaults() {
    let (defaults, suiteName) = isolatedDefaults()
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let writer = AIChatState(defaults: defaults)
    writer.mode = .detached

    let reader = AIChatState(defaults: defaults)
    XCTAssertEqual(reader.mode, .detached)
  }

  // MARK: - Visibility

  @MainActor
  func testIsVisibleDefaultsToFalse() {
    let (defaults, suiteName) = isolatedDefaults()
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let state = AIChatState(defaults: defaults)
    XCTAssertFalse(state.isVisible,
                   "Chat is hidden on first launch until the user opens it. " +
                   "If we ever want a 'show on first launch' tutorial, that " +
                   "decision lives elsewhere — `AIChatState` itself defaults hidden.")
  }

  @MainActor
  func testIsVisibleRoundTripsThroughDefaults() {
    let (defaults, suiteName) = isolatedDefaults()
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let writer = AIChatState(defaults: defaults)
    writer.isVisible = true

    let reader = AIChatState(defaults: defaults)
    XCTAssertTrue(reader.isVisible)
  }

  // MARK: - All-fields round-trip

  /// Belt-and-suspenders: a single sequence touching every
  /// persisted field, ensuring no field's `didSet` accidentally
  /// stomps another's persisted value (the kind of bug that
  /// `persist()` would silently introduce if it reads from a
  /// stale snapshot).
  @MainActor
  func testAllFieldsPersistTogether() {
    let (defaults, suiteName) = isolatedDefaults()
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let writer = AIChatState(defaults: defaults)
    writer.isVisible = true
    writer.mode = .detached
    writer.layoutMode = .push

    let reader = AIChatState(defaults: defaults)
    XCTAssertTrue(reader.isVisible)
    XCTAssertEqual(reader.mode, .detached)
    XCTAssertEqual(reader.layoutMode, .push)
  }

  // MARK: - Helpers

  /// Each test gets its own UUID-suffixed UserDefaults suite so
  /// concurrent tests can't observe each other's writes.
  private func isolatedDefaults() -> (UserDefaults, String) {
    let suiteName = "test.AIChatState.\(UUID().uuidString)"
    let defaults = UserDefaults(suiteName: suiteName)!
    // Defensive: in case a previous run with the same UUID
    // somehow lingered, start from empty state.
    defaults.removePersistentDomain(forName: suiteName)
    return (defaults, suiteName)
  }
}
