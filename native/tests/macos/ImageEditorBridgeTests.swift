import XCTest

/// Unit tests for ImageEditorBridge (Obj-C++ bridge wrapping C++ engine).
/// These test the bridge integration from Swift — the C++ core logic is
/// tested separately in crop_math_tests.cpp and image_editor_engine_tests.cpp.
class ImageEditorBridgeTests: XCTestCase {

    var bridge: ImageEditorBridge!

    override func setUpWithError() throws {
        bridge = ImageEditorBridge()
        XCTAssertNotNil(bridge, "Bridge should initialize successfully")

        // Simulate a container and loaded image.
        bridge.setContainerWidth(800, height: 600)
        bridge.setImageLoadedWidth(1920, height: 1080)
    }

    override func tearDownWithError() throws {
        bridge = nil
    }

    /// Convenience — force-unwrap state (safe in tests, crash = test failure).
    private var state: IEBState { bridge.currentState()! }

    // MARK: - Initialization

    func testInitialStateHasCropDimensions() {
        XCTAssertGreaterThan(state.cropWidth, 0, "Crop width should be set after image load")
        XCTAssertGreaterThan(state.cropHeight, 0, "Crop height should be set after image load")
    }

    func testInitialStateIsNotDirty() {
        XCTAssertFalse(state.isDirty, "State should be clean initially")
    }

    func testInitialStateCannotUndo() {
        XCTAssertFalse(state.canUndo, "Cannot undo with no history")
    }

    func testInitialStateCannotRedo() {
        XCTAssertFalse(state.canRedo, "Cannot redo with no history")
    }

    func testInitialStateNotInCropMode() {
        XCTAssertFalse(state.isCropMode, "Should start in view mode")
    }

    func testTemplatesArePopulated() {
        let templates = state.templates!
        XCTAssertGreaterThan(templates.count, 0, "Templates should be populated")

        // Verify expected templates exist.
        let labels = templates.map { $0.label ?? "" }
        XCTAssertTrue(labels.contains("16:9"), "Should have 16:9 template")
        XCTAssertTrue(labels.contains("4:3"), "Should have 4:3 template")
        XCTAssertTrue(labels.contains("1:1"), "Should have 1:1 template")
    }

    // MARK: - Crop Mode

    func testEnterCropMode() {
        bridge.enterCropMode()
        XCTAssertTrue(state.isCropMode, "Should be in crop mode after enterCropMode")
    }

    func testExitCropMode() {
        bridge.enterCropMode()
        bridge.exitCropMode()
        XCTAssertFalse(state.isCropMode, "Should exit crop mode")
    }

    // MARK: - Rotation

    func testRotateClockwise() {
        bridge.enterCropMode()
        bridge.rotateClockwise()
        XCTAssertEqual(state.rotation, 90, "Rotation should be 90 after clockwise rotate")
        XCTAssertTrue(state.isDirty, "State should be dirty after rotation")
        XCTAssertTrue(state.canUndo, "Should be able to undo after rotation")
    }

    func testRotateCounterClockwise() {
        bridge.enterCropMode()
        bridge.rotateCounterClockwise()
        XCTAssertEqual(state.rotation, 270, "Rotation should be 270 after counter-clockwise rotate")
    }

    func testFullRotationCycle() {
        bridge.enterCropMode()
        for _ in 0..<4 {
            bridge.rotateClockwise()
        }
        XCTAssertEqual(state.rotation % 360, 0, "Four 90-degree rotations should return to 0")
    }

    // MARK: - Flip

    func testFlipHorizontal() {
        bridge.enterCropMode()
        bridge.toggleFlipHorizontal()
        XCTAssertTrue(state.flipHorizontal, "Should be flipped horizontally")
        XCTAssertTrue(state.isDirty, "State should be dirty after flip")
    }

    func testFlipVertical() {
        bridge.enterCropMode()
        bridge.toggleFlipVertical()
        XCTAssertTrue(state.flipVertical, "Should be flipped vertically")
    }

    func testDoubleFlipReturnsToNormal() {
        bridge.enterCropMode()
        bridge.toggleFlipHorizontal()
        bridge.toggleFlipHorizontal()
        XCTAssertFalse(state.flipHorizontal, "Double flip should cancel out")
    }

    // MARK: - Straighten

    func testStraightenUpdatesState() {
        bridge.enterCropMode()
        bridge.setStraighten(15.0)
        XCTAssertEqual(state.straighten, 15.0, accuracy: 0.01, "Straighten should be 15 degrees")
    }

    func testCommitStraightenMakesUndoable() {
        bridge.enterCropMode()
        bridge.setStraighten(10.0)
        bridge.commitStraighten()
        XCTAssertTrue(state.canUndo, "Should be able to undo after committed straighten")
    }

    // MARK: - Aspect Ratio Templates

    func testApplyAspectRatio16_9() {
        bridge.enterCropMode()
        bridge.applyTemplate(withLabel: "16:9", ratio: 16.0 / 9.0)
        XCTAssertNotNil(state.activeTemplate, "Active template should be set")
        XCTAssertEqual(state.activeTemplate?.label, "16:9")

        // Verify crop dimensions maintain 16:9 ratio.
        let ratio = state.cropWidth / state.cropHeight
        XCTAssertEqual(ratio, 16.0 / 9.0, accuracy: 0.01, "Crop should have 16:9 ratio")
    }

    func testApplyFreeformTemplate() {
        // First apply a fixed ratio.
        bridge.enterCropMode()
        bridge.applyTemplate(withLabel: "1:1", ratio: 1.0)
        // Then switch to freeform.
        bridge.applyFreeformTemplate()
        XCTAssertNotNil(state.activeTemplate)
        XCTAssertTrue(state.activeTemplate?.isFreeform ?? false, "Should be freeform template")
    }

    // MARK: - Undo / Redo

    func testUndoRevertsRotation() {
        bridge.enterCropMode()
        let before = state.rotation
        bridge.rotateClockwise()
        bridge.undo()
        XCTAssertEqual(before, state.rotation, "Undo should revert rotation")
    }

    func testRedoReappliesRotation() {
        bridge.enterCropMode()
        bridge.rotateClockwise()
        bridge.undo()
        bridge.redo()
        XCTAssertEqual(state.rotation, 90, "Redo should re-apply rotation")
    }

    // MARK: - Reset

    func testResetClearsAllChanges() {
        bridge.enterCropMode()
        bridge.rotateClockwise()
        bridge.toggleFlipHorizontal()
        bridge.setStraighten(20.0)
        bridge.commitStraighten()

        bridge.reset()
        XCTAssertFalse(state.isDirty, "State should be clean after reset")
        XCTAssertFalse(state.flipHorizontal, "Flip should be reset")
        XCTAssertEqual(state.rotation, 0, "Rotation should be reset")
        XCTAssertEqual(state.straighten, 0.0, accuracy: 0.01, "Straighten should be reset")
    }

    // MARK: - Crop Result

    func testGetCropRectReturnsValidRect() {
        bridge.enterCropMode()
        let rect = bridge.getCropRect()!
        XCTAssertGreaterThan(rect.width, 0, "Crop width should be positive")
        XCTAssertGreaterThan(rect.height, 0, "Crop height should be positive")
        XCTAssertGreaterThanOrEqual(rect.x, 0, "Crop X should be non-negative")
        XCTAssertGreaterThanOrEqual(rect.y, 0, "Crop Y should be non-negative")
    }

    func testCropRectFitsWithinImage() {
        bridge.enterCropMode()
        let rect = bridge.getCropRect()!
        XCTAssertLessThanOrEqual(Int(rect.x) + Int(rect.width), 1920,
                                 "Crop should fit within image width")
        XCTAssertLessThanOrEqual(Int(rect.y) + Int(rect.height), 1080,
                                 "Crop should fit within image height")
    }

    // MARK: - Change Callback

    func testOnStateChangedCallbackFires() {
        let expectation = expectation(description: "State change callback")
        var callbackCount = 0

        bridge.onStateChanged = {
            callbackCount += 1
            if callbackCount == 1 {
                expectation.fulfill()
            }
        }

        bridge.enterCropMode()
        bridge.rotateClockwise()

        wait(for: [expectation], timeout: 1.0)
        XCTAssertGreaterThan(callbackCount, 0, "Callback should have fired")
    }

    // MARK: - Zoom

    func testInitialZoomIsFit() {
        XCTAssertTrue(state.isZoomFit, "Initial zoom should be fit mode")
    }

    func testSetZoomChangesValue() {
        bridge.setZoom(200)
        XCTAssertEqual(state.zoom, 200, accuracy: 1.0, "Zoom should be ~200%")
        XCTAssertFalse(state.isZoomFit, "Should not be in fit mode after manual zoom")
    }

    func testZoomToFitResetsToFitMode() {
        bridge.setZoom(300)
        bridge.zoomToFit()
        XCTAssertTrue(state.isZoomFit, "Should return to fit mode")
    }
}
