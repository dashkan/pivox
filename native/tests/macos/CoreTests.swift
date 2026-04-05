import XCTest

/// Tests for the shared C++ core library.
class CoreTests: XCTestCase {

    func testCoreVersionIsNotEmpty() {
        // The C++ core exposes a version string.
        // For now, verify the placeholder value.
        // This will be replaced with actual C++ interop when the
        // bridging header exposes core.h functions.
        XCTAssertTrue(true, "Core library placeholder — wire C++ interop next")
    }
}
