#pragma once

#include "image_editor_types.h"

namespace pivox {

/// Compute the minimum scale needed so a rotated image completely fills the
/// crop rect (no dead pixels). Core formula from the "fill-to-frame" algorithm.
double computeMinScale(double cropW, double cropH, double imgW, double imgH,
                       double angleRad);

/// Compute minimum scale accounting for perspective correction
/// (vertical/horizontal tilt). When perspVRad == 0 && perspHRad == 0,
/// degenerates to rotation-only. Uses corner-projection through inverse
/// perspective + inverse rotation.
double computeMinScaleWithPerspective(double cropW, double cropH, double imgW,
                                      double imgH, double angleRad,
                                      double perspVRad, double perspHRad);

/// Compute the maximum translation boundaries ("leash") for a given rotation,
/// perspective, and scale. Prevents dead pixels when panning.
struct TranslationBounds {
  double maxTx;
  double maxTy;
};
TranslationBounds computeTranslationBounds(double cropW, double cropH,
                                           double imgW, double imgH,
                                           double scale, double angleRad,
                                           double perspVRad = 0,
                                           double perspHRad = 0);

/// Clamp translation values to allowed boundaries.
struct ClampedTranslation {
  double tx;
  double ty;
};
ClampedTranslation clampTranslation(double tx, double ty, double maxTx,
                                    double maxTy);

/// Convert crop-as-viewport state to a CropRect in image pixel space.
/// Used for server-side processing output.
CropRect stateToImageCropRect(double cropW, double cropH, double imgW,
                              double imgH, double tx, double ty, double scale,
                              double angleRad);

/// Apply an aspect ratio template. Returns new crop dimensions that fit
/// within the image at the given rotation.
struct CropSize {
  double cropW;
  double cropH;
};
CropSize applyCropTemplate(double ratio, double currentCropW,
                           double currentCropH, double imgW, double imgH);

/// Resize crop dimensions from a handle drag.
CropSize resizeCropFromHandle(DragHandle handle, double deltaX, double deltaY,
                              double currentCropW, double currentCropH,
                              std::optional<double> aspectRatio);

/// Check if a proposed crop size is valid (image at current scale,
/// rotation, and perspective can fill it without dead pixels).
bool isCropSizeValid(double newCropW, double newCropH, double imgW, double imgH,
                     double scale, double angleRad, double perspVRad = 0,
                     double perspHRad = 0);

/// Convert rotation (degrees) + straighten (degrees) to radians.
inline double totalAngleRad(int rotation, double straighten) {
  return (rotation + straighten) * M_PI / 180.0;
}

/// Hit-test crop handles. Returns the handle under the point, or nullopt.
/// px, py are in crop-centered coordinates (0,0 = crop center).
std::optional<DragHandle> hitTestHandles(double px, double py, double cropW,
                                         double cropH, double hitRadius);

}  // namespace pivox
