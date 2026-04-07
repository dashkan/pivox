import XCTest

class ImageEditorUITests: XCTestCase {

    var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchEnvironment["USE_AUTH_EMULATOR"] = "1"
        app.launchEnvironment["UI_TESTING"] = "1"
        app.launchEnvironment["TEST_IMAGE_PATH"] = createTestImage()
        app.launch()

        // Register so we land on the main app.
        // UI_TESTING resets sticky tab → defaults to Operator.
        registerAndSignIn()

        // Navigate to Library — TEST_IMAGE_PATH auto-loads the image.
        let library = app.staticTexts["Library"]
        XCTAssertTrue(library.waitForExistence(timeout: 3))
        library.click()
    }

    override func tearDownWithError() throws {
        app = nil
    }

    // MARK: - Helpers

    /// Creates a 400x300 test PNG and returns its path.
    private func createTestImage() -> String {
        let size = NSSize(width: 400, height: 300)
        let image = NSImage(size: size)
        image.lockFocus()
        NSColor.systemBlue.setFill()
        NSRect(origin: .zero, size: size).fill()
        NSColor.white.setStroke()
        let line = NSBezierPath()
        line.move(to: .zero)
        line.line(to: NSPoint(x: size.width, y: size.height))
        line.lineWidth = 4
        line.stroke()
        image.unlockFocus()

        let tiff = image.tiffRepresentation!
        let bitmap = NSBitmapImageRep(data: tiff)!
        let png = bitmap.representation(using: .png, properties: [:])!
        let path = NSTemporaryDirectory() + "pivox-test-\(UUID().uuidString.prefix(8)).png"
        try! png.write(to: URL(fileURLWithPath: path))
        return path
    }

    private func uniqueEmail() -> String {
        "editor-\(UUID().uuidString.prefix(8).lowercased())@pivox.app"
    }

    /// Register a new account through the UI.
    private func registerAndSignIn() {
        let link = app.links["login-switch-register"].exists
            ? app.links["login-switch-register"]
            : app.buttons["login-switch-register"]
        guard link.waitForExistence(timeout: 5) else { return }
        link.click()

        let email = uniqueEmail()
        let emailField = app.textFields["register-email"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 3))
        emailField.click()
        emailField.typeText(email)

        app.textFields["register-display-name"].click()
        app.textFields["register-display-name"].typeText("Test")

        app.secureTextFields["register-password"].click()
        app.secureTextFields["register-password"].typeText("Testpass123!")

        app.secureTextFields["register-confirm-password"].click()
        app.secureTextFields["register-confirm-password"].typeText("Testpass123!")

        app.buttons["register-create-account"].click()

        // Wait for main app sidebar.
        let operator_ = app.staticTexts["Operator"]
        XCTAssertTrue(operator_.waitForExistence(timeout: 10),
                      "Should land on main app after registration")
    }

    /// Wait for image editor to be visible.
    /// Note: NSView accessibility identifiers don't propagate through SwiftUI
    /// containers, so we detect the editor by its toolbar Edit button.
    private func waitForEditor() {
        let editButton = app.buttons["edit-enter"]
        XCTAssertTrue(editButton.waitForExistence(timeout: 5),
                      "Image editor should be visible (Edit button in toolbar)")
    }

    // MARK: - View Mode Tests

    func testEditorOpensInViewMode() throws {
        waitForEditor()

        let editButton = app.buttons["edit-enter"]
        XCTAssertTrue(editButton.exists, "Edit button should be visible in view mode")

        let doneButton = app.buttons["edit-done"]
        XCTAssertFalse(doneButton.exists, "Done button should NOT be visible in view mode")
    }

    func testBackButtonClosesEditor() throws {
        waitForEditor()

        app.buttons["edit-back"].click()

        let openButton = app.buttons["library-open-image"]
        XCTAssertTrue(openButton.waitForExistence(timeout: 5),
                      "Should return to Library view after back")
    }

    // MARK: - Edit Mode Tests

    func testEditButtonEntersEditMode() throws {
        waitForEditor()

        app.buttons["edit-enter"].click()

        let doneButton = app.buttons["edit-done"]
        XCTAssertTrue(doneButton.waitForExistence(timeout: 3),
                      "Done button should appear in edit mode")

        let revertButton = app.buttons["edit-revert"]
        XCTAssertTrue(revertButton.exists, "Revert button should be visible in edit mode")
    }

    func testCropToolPanelVisibleInEditMode() throws {
        waitForEditor()
        app.buttons["edit-enter"].click()

        let straighten = app.otherElements["edit-straighten"]
        XCTAssertTrue(straighten.waitForExistence(timeout: 3),
                      "Straighten tool should be visible")

        XCTAssertTrue(app.otherElements["edit-flip-h"].exists, "Flip H should be visible")
        XCTAssertTrue(app.otherElements["edit-flip-v"].exists, "Flip V should be visible")
        XCTAssertTrue(app.otherElements["edit-aspect"].exists, "Aspect should be visible")
    }

    func testUndoRedoDisabledInitially() throws {
        waitForEditor()
        app.buttons["edit-enter"].click()

        let undo = app.buttons["edit-undo"]
        XCTAssertTrue(undo.waitForExistence(timeout: 3))
        XCTAssertFalse(undo.isEnabled, "Undo should be disabled initially")

        let redo = app.buttons["edit-redo"]
        XCTAssertTrue(redo.exists)
        XCTAssertFalse(redo.isEnabled, "Redo should be disabled initially")
    }

    func testRevertDisabledWhenClean() throws {
        waitForEditor()
        app.buttons["edit-enter"].click()

        let revert = app.buttons["edit-revert"]
        XCTAssertTrue(revert.waitForExistence(timeout: 3))
        XCTAssertFalse(revert.isEnabled, "Revert should be disabled when clean")
    }

    func testResetDisabledWhenClean() throws {
        waitForEditor()
        app.buttons["edit-enter"].click()

        let reset = app.buttons["edit-reset"]
        XCTAssertTrue(reset.waitForExistence(timeout: 3))
        XCTAssertFalse(reset.isEnabled, "Reset should be disabled when clean")
    }

    // MARK: - Done / Exit Tests

    func testDoneExitsEditMode() throws {
        waitForEditor()
        app.buttons["edit-enter"].click()

        let doneButton = app.buttons["edit-done"]
        XCTAssertTrue(doneButton.waitForExistence(timeout: 3))
        doneButton.click()

        let openButton = app.buttons["library-open-image"]
        XCTAssertTrue(openButton.waitForExistence(timeout: 5),
                      "Should return to Library after Done")
    }

    func testDoneShowsCropResult() throws {
        waitForEditor()
        app.buttons["edit-enter"].click()

        let doneButton = app.buttons["edit-done"]
        XCTAssertTrue(doneButton.waitForExistence(timeout: 3))
        doneButton.click()

        let cropResult = app.staticTexts["library-crop-result"]
        XCTAssertTrue(cropResult.waitForExistence(timeout: 5),
                      "Crop result should be displayed after Done")
    }

    func testSidebarRestoredAfterDone() throws {
        waitForEditor()
        app.buttons["edit-enter"].click()

        let doneButton = app.buttons["edit-done"]
        XCTAssertTrue(doneButton.waitForExistence(timeout: 3))
        doneButton.click()

        let openButton = app.buttons["library-open-image"]
        XCTAssertTrue(openButton.waitForExistence(timeout: 5))

        let operator_ = app.staticTexts["Operator"]
        XCTAssertTrue(operator_.waitForExistence(timeout: 3),
                      "Sidebar should be restored after exiting editor")
    }

    // MARK: - Zoom Controls

    func testZoomControlsExist() throws {
        waitForEditor()

        let zoom = app.otherElements["edit-zoom"]
        XCTAssertTrue(zoom.waitForExistence(timeout: 3),
                      "Zoom controls should be visible in view mode")

        app.buttons["edit-enter"].click()
        XCTAssertTrue(zoom.waitForExistence(timeout: 3),
                      "Zoom controls should be visible in edit mode")
    }

    // MARK: - Accessibility Tests

    func testCanvasIsDiscoverable() throws {
        waitForEditor()

        let canvas = app.images["image-edit-view"]
        XCTAssertTrue(canvas.waitForExistence(timeout: 5),
                      "Image edit canvas should be discoverable by its identifier")
        XCTAssertEqual(canvas.label, "Image Editor", "Canvas should have correct accessibility label")
    }
}
