import XCTest

/// Image editor UI tests — 2 focused tests using SKIP_AUTH (no emulator needed).
class ImageEditorUITests: XCTestCase {

    var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchEnvironment["UI_TESTING"] = "1"
        app.launchEnvironment["SKIP_AUTH"] = "1"
        app.launchEnvironment["TEST_IMAGE_PATH"] = createTestImage()
        app.launch()

        let library = app.staticTexts["Library"]
        XCTAssertTrue(library.waitForExistence(timeout: 3))
        library.click()
    }

    override func tearDownWithError() throws {
        app = nil
    }

    // MARK: - Helpers

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

    private func waitForEditor() {
        let editButton = app.buttons["edit-enter"]
        XCTAssertTrue(editButton.waitForExistence(timeout: 5),
                      "Image editor should be visible (Edit button in toolbar)")
    }

    private func enterEditMode() {
        app.buttons["edit-enter"].click()
        let doneButton = app.buttons["edit-done"]
        XCTAssertTrue(doneButton.waitForExistence(timeout: 3),
                      "Should enter edit mode (Done button visible)")
    }

    // MARK: - Test 1: Edit mode — elements, sliders, undo/redo

    func testEditModeAndSliders() throws {
        waitForEditor()

        // ── View mode ──
        XCTAssertTrue(app.buttons["edit-enter"].exists)
        XCTAssertFalse(app.buttons["edit-done"].exists)
        let canvas = app.images["image-edit-view"]
        XCTAssertTrue(canvas.waitForExistence(timeout: 3))
        XCTAssertEqual(canvas.label, "Image Editor")
        XCTAssertTrue(app.sliders["edit-zoom"].exists, "Zoom in view mode")

        // ── Enter edit mode ──
        enterEditMode()

        // All edit-mode elements present
        XCTAssertTrue(app.buttons["edit-revert"].exists)
        XCTAssertTrue(app.sliders["edit-zoom"].exists, "Zoom in edit mode")
        XCTAssertTrue(app.sliders["edit-straighten"].exists, "Straighten")
        XCTAssertTrue(app.sliders["edit-perspective-v"].exists, "Perspective V")
        XCTAssertTrue(app.sliders["edit-perspective-h"].exists, "Perspective H")
        XCTAssertTrue(app.buttons["edit-flip-h"].exists, "Flip H")
        XCTAssertTrue(app.buttons["edit-flip-v"].exists, "Flip V")
        XCTAssertTrue(app.buttons["edit-aspect"].exists, "Aspect")

        // Initial state — nothing dirty
        let undo = app.buttons["edit-undo"]
        let redo = app.buttons["edit-redo"]
        XCTAssertFalse(undo.isEnabled, "Undo disabled initially")
        XCTAssertFalse(redo.isEnabled, "Redo disabled initially")
        XCTAssertFalse(app.buttons["edit-revert"].isEnabled, "Revert disabled when clean")
        XCTAssertFalse(app.buttons["edit-reset"].isEnabled, "Reset disabled when clean")

        // ── Exercise each slider: adjust, verify value, undo, verify restored ──
        let sliders: [(XCUIElement, String)] = [
            (app.sliders["edit-straighten"], "Straighten"),
            (app.sliders["edit-perspective-v"], "Perspective V"),
            (app.sliders["edit-perspective-h"], "Perspective H"),
        ]

        for (slider, name) in sliders {
            // Initial value at center (0.5 normalized = 0 degrees)
            XCTAssertEqual(slider.normalizedSliderPosition, 0.5,
                           accuracy: 0.01, "\(name) starts at center")

            // Adjust to 75% (XCUITest drag has ~0.03 pixel rounding tolerance)
            slider.adjust(toNormalizedSliderPosition: 0.75)
            let adjusted = slider.normalizedSliderPosition
            XCTAssertEqual(adjusted, 0.75,
                           accuracy: 0.05, "\(name) near 0.75 after adjust")

            // State dirty
            XCTAssertTrue(undo.isEnabled, "Undo enabled after \(name)")
            XCTAssertTrue(app.buttons["edit-reset"].isEnabled, "Reset enabled after \(name)")

            // Undo reverts to 0.5
            undo.click()
            XCTAssertEqual(slider.normalizedSliderPosition, 0.5,
                           accuracy: 0.01, "\(name) at center after undo")
            XCTAssertTrue(redo.isEnabled, "Redo enabled after undoing \(name)")

            // Redo restores the adjusted value
            redo.click()
            XCTAssertEqual(slider.normalizedSliderPosition, adjusted,
                           accuracy: 0.01, "\(name) restored after redo")

            // Reset to center
            app.buttons["edit-reset"].click()
            XCTAssertEqual(slider.normalizedSliderPosition, 0.5,
                           accuracy: 0.01, "\(name) at center after reset")
        }
    }

    // MARK: - Test 2: Navigation — Back exits, Done exits with crop result + sidebar

    func testNavigationFlow() throws {
        waitForEditor()

        // ── Back → library ──
        app.buttons["edit-back"].click()
        let openButton = app.buttons["library-open-image"]
        XCTAssertTrue(openButton.waitForExistence(timeout: 5), "Back returns to Library")

        // ── Relaunch for Done test (didAutoLoad guard prevents re-load) ──
        app.terminate()
        app.launchEnvironment["TEST_IMAGE_PATH"] = createTestImage()
        app.launch()
        let library = app.staticTexts["Library"]
        XCTAssertTrue(library.waitForExistence(timeout: 3))
        library.click()
        waitForEditor()
        enterEditMode()

        // ── Done → library with crop result + sidebar ──
        app.buttons["edit-done"].click()
        XCTAssertTrue(openButton.waitForExistence(timeout: 5), "Done returns to Library")
        XCTAssertTrue(app.staticTexts["library-crop-result"].waitForExistence(timeout: 3),
                      "Crop result displayed")
        XCTAssertTrue(app.staticTexts["Operator"].waitForExistence(timeout: 3),
                      "Sidebar restored after Done")
    }
}
