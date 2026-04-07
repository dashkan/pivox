#include <gtest/gtest.h>
#include "crop_math.h"
#include <cmath>

using namespace pivox;

// ─────────────────────────────────────────────────────────────────────
// computeMinScale
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, MinScaleNoRotation) {
    // Full image crop at 0° → scale 1.0
    EXPECT_DOUBLE_EQ(computeMinScale(1920, 1080, 1920, 1080, 0), 1.0);
}

TEST(CropMathTest, MinScaleSmallCropNoRotation) {
    // Crop smaller than image → scale < 1.0
    double s = computeMinScale(960, 540, 1920, 1080, 0);
    EXPECT_DOUBLE_EQ(s, 0.5);
}

TEST(CropMathTest, MinScaleWithRotation) {
    // At 45° rotation, more image area is needed to fill the crop
    double s0 = computeMinScale(1000, 1000, 2000, 2000, 0);
    double s45 = computeMinScale(1000, 1000, 2000, 2000, M_PI / 4);
    EXPECT_GT(s45, s0);
}

TEST(CropMathTest, MinScaleAt90Degrees) {
    // 90° rotation of a non-square image: width and height swap
    double s = computeMinScale(1080, 1920, 1920, 1080, M_PI / 2);
    // At 90°, cos≈0 sin≈1 → requiredW = cropH, requiredH = cropW
    // requiredW/imgW = 1920/1920 = 1, requiredH/imgH = 1080/1080 = 1
    EXPECT_NEAR(s, 1.0, 0.001);
}

TEST(CropMathTest, MinScaleSymmetric) {
    // Positive and negative angles should give the same result
    double sPos = computeMinScale(800, 600, 1600, 1200, M_PI / 6);
    double sNeg = computeMinScale(800, 600, 1600, 1200, -M_PI / 6);
    EXPECT_DOUBLE_EQ(sPos, sNeg);
}

// ─────────────────────────────────────────────────────────────────────
// computeTranslationBounds
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, TranslationBoundsFullCropNoRotation) {
    // Crop = image at scale 1 → no room to move
    auto b = computeTranslationBounds(1920, 1080, 1920, 1080, 1.0, 0);
    EXPECT_NEAR(b.maxTx, 0, 0.001);
    EXPECT_NEAR(b.maxTy, 0, 0.001);
}

TEST(CropMathTest, TranslationBoundsSmallCrop) {
    // Half-size crop at full scale → can move in both directions
    auto b = computeTranslationBounds(960, 540, 1920, 1080, 1.0, 0);
    EXPECT_NEAR(b.maxTx, 480, 0.001);
    EXPECT_NEAR(b.maxTy, 270, 0.001);
}

TEST(CropMathTest, TranslationBoundsZoomedIn) {
    // Scale 2x with full crop → can pan by half the image size
    auto b = computeTranslationBounds(1920, 1080, 1920, 1080, 2.0, 0);
    EXPECT_NEAR(b.maxTx, 960, 0.001);
    EXPECT_NEAR(b.maxTy, 540, 0.001);
}

TEST(CropMathTest, TranslationBoundsNonNegative) {
    // Should never return negative bounds
    auto b = computeTranslationBounds(100, 100, 50, 50, 1.0, 0);
    EXPECT_GE(b.maxTx, 0);
    EXPECT_GE(b.maxTy, 0);
}

// ─────────────────────────────────────────────────────────────────────
// clampTranslation
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, ClampWithinBounds) {
    auto c = clampTranslation(50, -30, 100, 100);
    EXPECT_DOUBLE_EQ(c.tx, 50);
    EXPECT_DOUBLE_EQ(c.ty, -30);
}

TEST(CropMathTest, ClampExceedsBounds) {
    auto c = clampTranslation(200, -200, 100, 100);
    EXPECT_DOUBLE_EQ(c.tx, 100);
    EXPECT_DOUBLE_EQ(c.ty, -100);
}

TEST(CropMathTest, ClampZeroBounds) {
    auto c = clampTranslation(50, -50, 0, 0);
    EXPECT_DOUBLE_EQ(c.tx, 0);
    EXPECT_DOUBLE_EQ(c.ty, 0);
}

// ─────────────────────────────────────────────────────────────────────
// stateToImageCropRect
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, CropRectNoTransformFullImage) {
    // Full image, no rotation, no translation, scale 1
    auto r = stateToImageCropRect(1920, 1080, 1920, 1080, 0, 0, 1.0, 0);
    EXPECT_EQ(r.x, 0);
    EXPECT_EQ(r.y, 0);
    EXPECT_EQ(r.width, 1920);
    EXPECT_EQ(r.height, 1080);
}

TEST(CropMathTest, CropRectCenteredSmallCrop) {
    // Half-size crop centered (no translation)
    auto r = stateToImageCropRect(960, 540, 1920, 1080, 0, 0, 1.0, 0);
    EXPECT_EQ(r.x, 480);
    EXPECT_EQ(r.y, 270);
    EXPECT_EQ(r.width, 960);
    EXPECT_EQ(r.height, 540);
}

TEST(CropMathTest, CropRectWithTranslation) {
    // Translated crop
    auto r = stateToImageCropRect(960, 540, 1920, 1080, 100, 50, 1.0, 0);
    // tx=100 means image moved right by 100px → crop moves left
    EXPECT_EQ(r.x, 480 - 100);
    EXPECT_EQ(r.y, 270 - 50);
    EXPECT_EQ(r.width, 960);
    EXPECT_EQ(r.height, 540);
}

TEST(CropMathTest, CropRectWithScale) {
    // Scale 2x → crop covers half the area in image space
    auto r = stateToImageCropRect(1920, 1080, 1920, 1080, 0, 0, 2.0, 0);
    EXPECT_EQ(r.width, 960);
    EXPECT_EQ(r.height, 540);
}

// ─────────────────────────────────────────────────────────────────────
// applyCropTemplate
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, TemplateSquareOnLandscape) {
    auto s = applyCropTemplate(1.0, 1920, 1080, 1920, 1080);
    EXPECT_DOUBLE_EQ(s.cropW, 1080);
    EXPECT_DOUBLE_EQ(s.cropH, 1080);
}

TEST(CropMathTest, Template16x9OnSquare) {
    auto s = applyCropTemplate(16.0 / 9.0, 1000, 1000, 1000, 1000);
    EXPECT_DOUBLE_EQ(s.cropW, 1000);
    EXPECT_NEAR(s.cropH, 1000.0 / (16.0 / 9.0), 0.001);
}

TEST(CropMathTest, Template9x16OnLandscape) {
    auto s = applyCropTemplate(9.0 / 16.0, 1920, 1080, 1920, 1080);
    // 9:16 on a landscape image → height-limited
    EXPECT_NEAR(s.cropH, 1080, 0.001);
    EXPECT_NEAR(s.cropW, 1080.0 * 9.0 / 16.0, 0.001);
}

// ─────────────────────────────────────────────────────────────────────
// resizeCropFromHandle
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, ResizeSEGrows) {
    auto s = resizeCropFromHandle(DragHandle::SE, 50, 50, 500, 500, std::nullopt);
    EXPECT_DOUBLE_EQ(s.cropW, 600);  // +50*2
    EXPECT_DOUBLE_EQ(s.cropH, 600);
}

TEST(CropMathTest, ResizeNWShrinks) {
    auto s = resizeCropFromHandle(DragHandle::NW, 50, 50, 500, 500, std::nullopt);
    EXPECT_DOUBLE_EQ(s.cropW, 400);  // -50*2
    EXPECT_DOUBLE_EQ(s.cropH, 400);
}

TEST(CropMathTest, ResizeEnforcesMinimum) {
    auto s = resizeCropFromHandle(DragHandle::NW, 300, 300, 500, 500, std::nullopt);
    EXPECT_GE(s.cropW, crop_constants::kMinCropSize);
    EXPECT_GE(s.cropH, crop_constants::kMinCropSize);
}

TEST(CropMathTest, ResizeWithAspectRatio) {
    auto s = resizeCropFromHandle(DragHandle::E, 100, 0, 500, 500, 16.0 / 9.0);
    // East handle: width changes, height follows ratio
    double expectedH = s.cropW / (16.0 / 9.0);
    EXPECT_NEAR(s.cropH, expectedH, 0.001);
}

TEST(CropMathTest, ResizeMoveDoesNotResize) {
    auto s = resizeCropFromHandle(DragHandle::Move, 100, 100, 500, 300, std::nullopt);
    EXPECT_DOUBLE_EQ(s.cropW, 500);
    EXPECT_DOUBLE_EQ(s.cropH, 300);
}

// ─────────────────────────────────────────────────────────────────────
// isCropSizeValid
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, ValidCropFullImage) {
    EXPECT_TRUE(isCropSizeValid(1920, 1080, 1920, 1080, 1.0, 0));
}

TEST(CropMathTest, InvalidCropTooLarge) {
    EXPECT_FALSE(isCropSizeValid(3000, 3000, 1920, 1080, 1.0, 0));
}

TEST(CropMathTest, ValidCropWithScale) {
    // Scale 2x → crop can be up to 2x image size
    EXPECT_TRUE(isCropSizeValid(3840, 2160, 1920, 1080, 2.0, 0));
}

// ─────────────────────────────────────────────────────────────────────
// hitTestHandles
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, HitTestCornerNW) {
    auto h = hitTestHandles(-250, -150, 500, 300, 16);
    ASSERT_TRUE(h.has_value());
    EXPECT_EQ(*h, DragHandle::NW);
}

TEST(CropMathTest, HitTestEdgeN) {
    auto h = hitTestHandles(0, -150, 500, 300, 16);
    ASSERT_TRUE(h.has_value());
    EXPECT_EQ(*h, DragHandle::N);
}

TEST(CropMathTest, HitTestCenter) {
    auto h = hitTestHandles(0, 0, 500, 300, 16);
    ASSERT_TRUE(h.has_value());
    EXPECT_EQ(*h, DragHandle::Move);
}

TEST(CropMathTest, HitTestOutside) {
    auto h = hitTestHandles(500, 500, 500, 300, 16);
    EXPECT_FALSE(h.has_value());
}

TEST(CropMathTest, HitTestNearCornerButOutsideRadius) {
    // Just outside the handle hit radius
    auto h = hitTestHandles(-250 + 20, -150 + 20, 500, 300, 16);
    // Distance = sqrt(20^2 + 20^2) ≈ 28.3 > 16 → not a corner hit
    // But inside crop rect → move
    ASSERT_TRUE(h.has_value());
    EXPECT_EQ(*h, DragHandle::Move);
}

// ─────────────────────────────────────────────────────────────────────
// totalAngleRad
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, TotalAngleRadZero) {
    EXPECT_DOUBLE_EQ(totalAngleRad(0, 0), 0);
}

TEST(CropMathTest, TotalAngleRad90) {
    EXPECT_NEAR(totalAngleRad(90, 0), M_PI / 2, 0.0001);
}

TEST(CropMathTest, TotalAngleRadWithStraighten) {
    EXPECT_NEAR(totalAngleRad(0, 45), M_PI / 4, 0.0001);
}

TEST(CropMathTest, TotalAngleRadCombined) {
    EXPECT_NEAR(totalAngleRad(90, -10), 80.0 * M_PI / 180.0, 0.0001);
}

// ─────────────────────────────────────────────────────────────────────
// computeMinScaleWithPerspective
// ─────────────────────────────────────────────────────────────────────

TEST(CropMathTest, PerspectiveMinScale_ZeroTilt_MatchesRotationOnly) {
    // With no perspective, should match the existing rotation-only formula
    double sRot = computeMinScale(1000, 800, 2000, 1600, M_PI / 6);
    double sPersp = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                    M_PI / 6, 0, 0);
    EXPECT_NEAR(sPersp, sRot, 0.001);
}

TEST(CropMathTest, PerspectiveMinScale_ZeroTilt_NoRotation_MatchesExact) {
    double sRot = computeMinScale(1920, 1080, 1920, 1080, 0);
    double sPersp = computeMinScaleWithPerspective(1920, 1080, 1920, 1080, 0, 0, 0);
    EXPECT_NEAR(sPersp, sRot, 0.001);
}

TEST(CropMathTest, PerspectiveMinScale_VerticalTilt_ScalesUp) {
    double s0 = computeMinScaleWithPerspective(1000, 1000, 2000, 2000, 0, 0, 0);
    double sV = computeMinScaleWithPerspective(1000, 1000, 2000, 2000,
                                                0, 20 * M_PI / 180, 0);
    EXPECT_GT(sV, s0);
}

TEST(CropMathTest, PerspectiveMinScale_HorizontalTilt_ScalesUp) {
    double s0 = computeMinScaleWithPerspective(1000, 1000, 2000, 2000, 0, 0, 0);
    double sH = computeMinScaleWithPerspective(1000, 1000, 2000, 2000,
                                                0, 0, 20 * M_PI / 180);
    EXPECT_GT(sH, s0);
}

TEST(CropMathTest, PerspectiveMinScale_BothAxes_LargerThanEither) {
    double sV = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                0, 15 * M_PI / 180, 0);
    double sH = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                0, 0, 15 * M_PI / 180);
    double sBoth = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                   0, 15 * M_PI / 180,
                                                   15 * M_PI / 180);
    EXPECT_GE(sBoth, sV - 0.001);
    EXPECT_GE(sBoth, sH - 0.001);
}

TEST(CropMathTest, PerspectiveMinScale_SymmetricPositiveNegative) {
    double sPos = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                  0, 25 * M_PI / 180, 0);
    double sNeg = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                  0, -25 * M_PI / 180, 0);
    EXPECT_NEAR(sPos, sNeg, 0.001);
}

TEST(CropMathTest, PerspectiveMinScale_WithRotation_Combined) {
    // Perspective + rotation should produce a larger scale than either alone
    double sRotOnly = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                      M_PI / 6, 0, 0);
    double sPerspOnly = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                        0, 20 * M_PI / 180, 0);
    double sCombined = computeMinScaleWithPerspective(1000, 800, 2000, 1600,
                                                       M_PI / 6,
                                                       20 * M_PI / 180, 0);
    EXPECT_GE(sCombined, sRotOnly - 0.001);
    EXPECT_GE(sCombined, sPerspOnly - 0.001);
}
