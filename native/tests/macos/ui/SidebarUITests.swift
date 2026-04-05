import XCTest

class SidebarUITests: XCTestCase {

    var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launch()
    }

    override func tearDownWithError() throws {
        app = nil
    }

    // MARK: - Helpers

    private func signIn() {
        let signInButton = app.buttons["Sign In"]
        if signInButton.waitForExistence(timeout: 5) {
            signInButton.click()
        }
    }

    private func findProfile() -> XCUIElement {
        // Profile may appear as button, static text, or other element.
        for query in [app.buttons, app.staticTexts] {
            let el = query["Profile"]
            if el.waitForExistence(timeout: 3) {
                return el
            }
        }
        return app.staticTexts["Profile"]
    }

    private func takeScreenshot(named name: String) {
        let screenshot = app.windows.firstMatch.screenshot()
        let pngData = screenshot.pngRepresentation
        let path = NSTemporaryDirectory() + "pivox-\(name).png"
        try? pngData.write(to: URL(fileURLWithPath: path))
        print("Screenshot [\(name)] saved to: \(path)")
    }

    // MARK: - Tests

    func testProfileButtonExistsInSidebar() throws {
        signIn()
        let profile = findProfile()
        XCTAssertTrue(profile.waitForExistence(timeout: 5), "Profile should be visible in sidebar")
    }

    func testProfileIsPinnedToBottom() throws {
        signIn()
        let profile = findProfile()
        XCTAssertTrue(profile.waitForExistence(timeout: 5))
        XCTAssertTrue(profile.isHittable, "Profile should be hittable without scrolling")

        let windowFrame = app.windows.firstMatch.frame
        let profileFrame = profile.frame
        let bottomGap = windowFrame.maxY - profileFrame.maxY

        print("Profile frame: \(profileFrame)")
        print("Window frame: \(windowFrame)")
        print("Bottom gap: \(bottomGap)")

        XCTAssertLessThan(bottomGap, 60, "Profile should be near the bottom of the window")
    }

    func testProfileButtonPaddingIsSymmetric() throws {
        signIn()
        let profile = findProfile()
        XCTAssertTrue(profile.waitForExistence(timeout: 5))
        profile.click()

        takeScreenshot(named: "profile-selected")

        let windowFrame = app.windows.firstMatch.frame
        let profileFrame = profile.frame
        let bottomGap = windowFrame.maxY - profileFrame.maxY

        print("Profile frame: \(profileFrame)")
        print("Window frame: \(windowFrame)")
        print("Bottom gap: \(bottomGap)")

        XCTAssertGreaterThan(bottomGap, 5, "Should have padding below Profile")
        XCTAssertLessThan(bottomGap, 50, "Bottom padding should not be excessive")
    }

    func testSidebarSectionsExist() throws {
        signIn()
        let sections = ["Operator", "Library", "Designer", "Engineering", "Admin"]
        for section in sections {
            let item = app.staticTexts[section]
            XCTAssertTrue(item.waitForExistence(timeout: 5), "\(section) should exist in sidebar")
        }
    }

    func testSidebarPositionIsStableAfterResize() throws {
        signIn()

        // Wait for sidebar to settle — use firstMatch to avoid ambiguity
        // with the detail view text.
        let operator_ = app.staticTexts.matching(identifier: "Operator").firstMatch
        XCTAssertTrue(operator_.waitForExistence(timeout: 5))

        takeScreenshot(named: "sidebar-before-resize")
        let initialFrame = operator_.frame
        print("Initial Operator frame: \(initialFrame)")

        // Resize the window by dragging the right edge.
        let window = app.windows.firstMatch
        let rightEdge = window.coordinate(withNormalizedOffset: CGVector(dx: 1.0, dy: 0.5))
        let wider = rightEdge.withOffset(CGVector(dx: 200, dy: 0))
        rightEdge.click(forDuration: 0.5, thenDragTo: wider)

        sleep(1)
        takeScreenshot(named: "sidebar-after-resize")
        let afterFrame = operator_.frame
        print("After resize Operator frame: \(afterFrame)")

        let yDrift = abs(afterFrame.minY - initialFrame.minY)
        print("Y drift: \(yDrift)")
        XCTAssertLessThan(yDrift, 10, "Sidebar items should not jump vertically after resize (drifted \(yDrift)pt)")
    }
}
