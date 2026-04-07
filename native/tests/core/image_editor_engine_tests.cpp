#include <gtest/gtest.h>
#include "image_editor_engine.h"
#include <cmath>

using namespace pivox;

class ImageEditorEngineTest : public ::testing::Test {
protected:
    ImageEditorEngine engine;

    void SetUp() override {
        // Simulate a 1920x1080 image loaded.
        engine.setImageLoaded(1920, 1080);
    }
};

// ─────────────────────────────────────────────────────────────────────
// Initial state
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, InitialStateAfterImageLoad) {
    auto& s = engine.state();
    EXPECT_EQ(s.naturalWidth, 1920);
    EXPECT_EQ(s.naturalHeight, 1080);
    EXPECT_EQ(s.imageStatus, ImageEditorState::ImageStatus::Loaded);
    EXPECT_DOUBLE_EQ(s.cropWidth, 1920);
    EXPECT_DOUBLE_EQ(s.cropHeight, 1080);
    EXPECT_EQ(s.rotation, 0);
    EXPECT_DOUBLE_EQ(s.straighten, 0);
    EXPECT_DOUBLE_EQ(s.scale, 1.0);
    EXPECT_DOUBLE_EQ(s.tx, 0);
    EXPECT_DOUBLE_EQ(s.ty, 0);
    EXPECT_FALSE(s.flipHorizontal);
    EXPECT_FALSE(s.flipVertical);
    EXPECT_FALSE(s.canUndo);
    EXPECT_FALSE(s.canRedo);
    EXPECT_FALSE(s.isDirty);
    EXPECT_EQ(s.mode, EditorMode::View);
}

TEST_F(ImageEditorEngineTest, InitialCropRectIsFullImage) {
    auto r = engine.getCropRect();
    EXPECT_EQ(r.x, 0);
    EXPECT_EQ(r.y, 0);
    EXPECT_EQ(r.width, 1920);
    EXPECT_EQ(r.height, 1080);
}

// ─────────────────────────────────────────────────────────────────────
// Rotation
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, RotateClockwise) {
    engine.rotateClockwise();
    EXPECT_EQ(engine.state().rotation, 90);
    EXPECT_TRUE(engine.state().canUndo);
    EXPECT_TRUE(engine.state().isDirty);
}

TEST_F(ImageEditorEngineTest, RotateClockwiseFourTimes) {
    engine.rotateClockwise();
    engine.rotateClockwise();
    engine.rotateClockwise();
    engine.rotateClockwise();
    EXPECT_EQ(engine.state().rotation, 0);
}

TEST_F(ImageEditorEngineTest, RotateCounterClockwise) {
    engine.rotateCounterClockwise();
    EXPECT_EQ(engine.state().rotation, 270);
}

TEST_F(ImageEditorEngineTest, RotateAdjustsMinScale) {
    // Full crop of non-square image. At 0° scale is 1.0.
    // After rotating, min scale increases to cover the crop.
    double initialScale = engine.state().scale;
    engine.rotateClockwise();  // 90°
    // Scale should have adjusted (for a non-square crop on non-square image).
    EXPECT_GE(engine.state().scale, initialScale);
}

// ─────────────────────────────────────────────────────────────────────
// Flip
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, FlipHorizontal) {
    engine.toggleFlipHorizontal();
    EXPECT_TRUE(engine.state().flipHorizontal);
    EXPECT_TRUE(engine.state().isDirty);
}

TEST_F(ImageEditorEngineTest, FlipHorizontalTwiceRestores) {
    engine.toggleFlipHorizontal();
    engine.toggleFlipHorizontal();
    EXPECT_FALSE(engine.state().flipHorizontal);
}

TEST_F(ImageEditorEngineTest, FlipVertical) {
    engine.toggleFlipVertical();
    EXPECT_TRUE(engine.state().flipVertical);
}

// ─────────────────────────────────────────────────────────────────────
// Straighten
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, StraightenSetsValue) {
    engine.setStraighten(15.0);
    EXPECT_DOUBLE_EQ(engine.state().straighten, 15.0);
}

TEST_F(ImageEditorEngineTest, StraightenClamps) {
    engine.setStraighten(60.0);
    EXPECT_DOUBLE_EQ(engine.state().straighten, 45.0);
    engine.setStraighten(-60.0);
    EXPECT_DOUBLE_EQ(engine.state().straighten, -45.0);
}

TEST_F(ImageEditorEngineTest, StraightenDoesNotPushHistoryUntilCommit) {
    engine.setStraighten(10.0);
    EXPECT_FALSE(engine.state().canUndo);  // No history yet
    engine.commitStraighten();
    EXPECT_TRUE(engine.state().canUndo);
}

TEST_F(ImageEditorEngineTest, StraightenCommitMakesDirty) {
    engine.setStraighten(10.0);
    engine.commitStraighten();
    EXPECT_TRUE(engine.state().isDirty);
}

// ─────────────────────────────────────────────────────────────────────
// Templates
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, ApplySquareTemplate) {
    CropTemplate square { "1:1", 1.0 };
    engine.applyTemplate(square);
    EXPECT_DOUBLE_EQ(engine.state().cropWidth, 1080);
    EXPECT_DOUBLE_EQ(engine.state().cropHeight, 1080);
    EXPECT_TRUE(engine.state().isDirty);
}

TEST_F(ImageEditorEngineTest, Apply16x9Template) {
    CropTemplate widescreen { "16:9", 16.0 / 9.0 };
    engine.applyTemplate(widescreen);
    EXPECT_NEAR(engine.state().cropWidth / engine.state().cropHeight,
                16.0 / 9.0, 0.001);
}

TEST_F(ImageEditorEngineTest, ApplyFreeformTemplate) {
    CropTemplate free { "Free", std::nullopt };
    double origW = engine.state().cropWidth;
    double origH = engine.state().cropHeight;
    engine.applyTemplate(free);
    // Freeform doesn't change dimensions.
    EXPECT_DOUBLE_EQ(engine.state().cropWidth, origW);
    EXPECT_DOUBLE_EQ(engine.state().cropHeight, origH);
}

// ─────────────────────────────────────────────────────────────────────
// Undo / Redo
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, UndoRestoresPreviousState) {
    engine.rotateClockwise();
    EXPECT_EQ(engine.state().rotation, 90);
    engine.undo();
    EXPECT_EQ(engine.state().rotation, 0);
    EXPECT_FALSE(engine.state().isDirty);
}

TEST_F(ImageEditorEngineTest, RedoRestoresUndoneState) {
    engine.rotateClockwise();
    engine.undo();
    EXPECT_EQ(engine.state().rotation, 0);
    engine.redo();
    EXPECT_EQ(engine.state().rotation, 90);
}

TEST_F(ImageEditorEngineTest, UndoOnEmptyHistoryDoesNothing) {
    int rotation = engine.state().rotation;
    engine.undo();
    EXPECT_EQ(engine.state().rotation, rotation);
}

TEST_F(ImageEditorEngineTest, NewActionClearsFuture) {
    engine.rotateClockwise();       // 90
    engine.rotateClockwise();       // 180
    engine.undo();                  // back to 90
    EXPECT_TRUE(engine.state().canRedo);
    engine.toggleFlipHorizontal();  // new action
    EXPECT_FALSE(engine.state().canRedo);
}

TEST_F(ImageEditorEngineTest, HistoryRespectesMaxDepth) {
    ImageEditorEngine smallHistory(ImageEditorEngine::Options{ {}, std::nullopt, 3 });
    smallHistory.setImageLoaded(1000, 1000);

    smallHistory.rotateClockwise();   // 90
    smallHistory.rotateClockwise();   // 180
    smallHistory.rotateClockwise();   // 270
    smallHistory.rotateClockwise();   // 0

    // Only 3 levels of undo available.
    smallHistory.undo(); // → 270
    smallHistory.undo(); // → 180
    smallHistory.undo(); // → 90
    EXPECT_EQ(smallHistory.state().rotation, 90);
    EXPECT_FALSE(smallHistory.state().canUndo);
}

// ─────────────────────────────────────────────────────────────────────
// Reset
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, ResetRestoresInitialState) {
    engine.rotateClockwise();
    engine.toggleFlipHorizontal();
    engine.reset();
    EXPECT_EQ(engine.state().rotation, 0);
    EXPECT_FALSE(engine.state().flipHorizontal);
}

TEST_F(ImageEditorEngineTest, ResetIsDirtyFalse) {
    engine.rotateClockwise();
    engine.reset();
    EXPECT_FALSE(engine.state().isDirty);
}

TEST_F(ImageEditorEngineTest, ResetCanBeUndone) {
    engine.rotateClockwise();
    engine.reset();
    EXPECT_TRUE(engine.state().canUndo);
    engine.undo();
    EXPECT_EQ(engine.state().rotation, 90);
}

// ─────────────────────────────────────────────────────────────────────
// Zoom
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, ZoomIn) {
    double initial = engine.state().zoom;
    engine.zoomIn();
    EXPECT_GT(engine.state().zoom, initial);
    EXPECT_EQ(engine.state().zoomMode, ZoomMode::Manual);
}

TEST_F(ImageEditorEngineTest, ZoomOut) {
    engine.setZoom(200);
    engine.zoomOut();
    EXPECT_LT(engine.state().zoom, 200);
}

TEST_F(ImageEditorEngineTest, ZoomToFit) {
    engine.setZoom(300);
    engine.zoomToFit();
    EXPECT_DOUBLE_EQ(engine.state().zoom, 100);
    EXPECT_EQ(engine.state().zoomMode, ZoomMode::Fit);
}

TEST_F(ImageEditorEngineTest, ZoomClampsToLimits) {
    engine.setZoom(10000);
    EXPECT_DOUBLE_EQ(engine.state().zoom, zoom_constants::kZoomMax);
    engine.setZoom(-50);
    EXPECT_DOUBLE_EQ(engine.state().zoom, zoom_constants::kZoomMin);
}

// ─────────────────────────────────────────────────────────────────────
// Mode
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, EnterCropMode) {
    engine.enterCropMode();
    EXPECT_EQ(engine.state().mode, EditorMode::Crop);
}

TEST_F(ImageEditorEngineTest, ExitCropMode) {
    engine.enterCropMode();
    engine.exitCropMode();
    EXPECT_EQ(engine.state().mode, EditorMode::View);
}

// ─────────────────────────────────────────────────────────────────────
// Resize mode
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, SetResizeMode) {
    engine.setResizeMode(ResizeMode::Cover);
    EXPECT_EQ(engine.state().resizeMode, ResizeMode::Cover);
    EXPECT_TRUE(engine.state().isDirty);
}

// ─────────────────────────────────────────────────────────────────────
// Change callback
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, ChangeCallbackFires) {
    int callCount = 0;
    engine.onChange([&](const ImageEditorState&) { callCount++; });
    engine.rotateClockwise();
    EXPECT_GT(callCount, 0);
}

// ─────────────────────────────────────────────────────────────────────
// Image error
// ─────────────────────────────────────────────────────────────────────

TEST(ImageEditorEngineStandaloneTest, ImageErrorSetsState) {
    ImageEditorEngine eng;
    eng.setImageError("Network error");
    EXPECT_EQ(eng.state().imageStatus, ImageEditorState::ImageStatus::Error);
    EXPECT_EQ(eng.state().imageError, "Network error");
}

// ─────────────────────────────────────────────────────────────────────
// getCropRect after transforms
// ─────────────────────────────────────────────────────────────────────

TEST_F(ImageEditorEngineTest, CropRectAfterSquareTemplate) {
    engine.applyTemplate(CropTemplate{ "1:1", 1.0 });
    auto r = engine.getCropRect();
    // Square crop centered on 1920x1080 image
    EXPECT_EQ(r.width, 1080);
    EXPECT_EQ(r.height, 1080);
    EXPECT_EQ(r.x, (1920 - 1080) / 2);
    EXPECT_EQ(r.y, 0);
}

// ─────────────────────────────────────────────────────────────────────
// Default templates
// ─────────────────────────────────────────────────────────────────────

TEST(ImageEditorEngineStandaloneTest, DefaultTemplatesIncludeFree) {
    ImageEditorEngine eng;
    auto& templates = eng.state().templates;
    ASSERT_FALSE(templates.empty());
    EXPECT_EQ(templates[0].label, "Free");
    EXPECT_FALSE(templates[0].ratio.has_value());
}

TEST(ImageEditorEngineStandaloneTest, CustomTemplatesPreserved) {
    ImageEditorEngine::Options opts;
    opts.templates = { CropTemplate{"16:9", 16.0/9.0}, CropTemplate{"4:3", 4.0/3.0} };
    ImageEditorEngine eng(opts);
    auto& templates = eng.state().templates;
    EXPECT_EQ(templates.size(), 3);  // Free + 2 custom
    EXPECT_EQ(templates[0].label, "Free");
    EXPECT_EQ(templates[1].label, "16:9");
    EXPECT_EQ(templates[2].label, "4:3");
}
