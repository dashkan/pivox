#include "crop_math.h"
#include <algorithm>
#include <cmath>
#include <limits>

namespace pivox {

double computeMinScale(double cropW, double cropH,
                       double imgW, double imgH,
                       double angleRad) {
    double absCos = std::abs(std::cos(angleRad));
    double absSin = std::abs(std::sin(angleRad));

    double requiredW = cropW * absCos + cropH * absSin;
    double requiredH = cropW * absSin + cropH * absCos;

    return std::max(requiredW / imgW, requiredH / imgH);
}

double computeMinScaleWithPerspective(double cropW, double cropH,
                                       double imgW, double imgH,
                                       double angleRad,
                                       double perspVRad, double perspHRad) {
    // Per-axis focal length: separate multipliers let us match Photos.app intensity.
    double base = std::max(cropW, cropH);
    double dV = base * perspective_constants::kFocalLengthMultiplierV;
    double dH = base * perspective_constants::kFocalLengthMultiplierH;

    double cosA = std::cos(-angleRad);
    double sinA = std::sin(-angleRad);
    double tanV = std::tan(perspVRad);
    double tanH = std::tan(perspHRad);
    double cosV = std::cos(perspVRad);
    double cosH = std::cos(perspHRad);

    double cornersX[] = { -cropW / 2,  cropW / 2,  cropW / 2, -cropW / 2 };
    double cornersY[] = { -cropH / 2, -cropH / 2,  cropH / 2,  cropH / 2 };

    double maxS = 0.0;
    for (int i = 0; i < 4; ++i) {
        // 1. Un-rotate crop corner back to image axes
        double qx = cornersX[i] * cosA - cornersY[i] * sinA;
        double qy = cornersX[i] * sinA + cornersY[i] * cosA;

        // 2. Inverse perspective projection (separate focal lengths per axis)
        double w = 1.0 - (qx * tanH / dH + qy * tanV / dV);
        if (w < 0.1) w = 0.1;  // prevent singularity near 90°

        double ux = (qx / cosH) / w;
        double uy = (qy / cosV) / w;

        // 3. Scale needed for this corner to fit inside image bounds
        double sx = std::abs(ux) / (imgW / 2.0);
        double sy = std::abs(uy) / (imgH / 2.0);
        maxS = std::max({maxS, sx, sy});
    }
    return maxS;
}

TranslationBounds computeTranslationBounds(double cropW, double cropH,
                                            double imgW, double imgH,
                                            double scale, double angleRad,
                                            double perspVRad, double perspHRad) {
    // Perspective is accounted for through the increased scale (from
    // computeMinScaleWithPerspective). The affine bounds are conservative.
    (void)perspVRad;
    (void)perspHRad;
    double absCos = std::abs(std::cos(angleRad));
    double absSin = std::abs(std::sin(angleRad));

    double hIW = (imgW * scale) / 2.0;
    double hIH = (imgH * scale) / 2.0;
    double hCW = cropW / 2.0;
    double hCH = cropH / 2.0;

    constexpr double eps = 1e-10;
    double txLimit1 = absCos > eps ? (hIW - (hCW * absCos + hCH * absSin)) / absCos
                                   : std::numeric_limits<double>::infinity();
    double txLimit2 = absSin > eps ? (hIH - (hCW * absSin + hCH * absCos)) / absSin
                                   : std::numeric_limits<double>::infinity();
    double maxTx = std::max(0.0, std::min(txLimit1, txLimit2));

    double tyLimit1 = absSin > eps ? (hIW - (hCW * absCos + hCH * absSin)) / absSin
                                   : std::numeric_limits<double>::infinity();
    double tyLimit2 = absCos > eps ? (hIH - (hCW * absSin + hCH * absCos)) / absCos
                                   : std::numeric_limits<double>::infinity();
    double maxTy = std::max(0.0, std::min(tyLimit1, tyLimit2));

    return { maxTx, maxTy };
}

ClampedTranslation clampTranslation(double tx, double ty,
                                     double maxTx, double maxTy) {
    return {
        std::max(-maxTx, std::min(tx, maxTx)),
        std::max(-maxTy, std::min(ty, maxTy)),
    };
}

CropRect stateToImageCropRect(double cropW, double cropH,
                               double imgW, double imgH,
                               double tx, double ty,
                               double scale, double angleRad) {
    double cosA = std::cos(-angleRad);
    double sinA = std::sin(-angleRad);

    double imgOffsetX = (-tx / scale) * cosA - (-ty / scale) * sinA;
    double imgOffsetY = (-tx / scale) * sinA + (-ty / scale) * cosA;

    double cropCenterX = imgW / 2.0 + imgOffsetX;
    double cropCenterY = imgH / 2.0 + imgOffsetY;

    double unscaledCropW = cropW / scale;
    double unscaledCropH = cropH / scale;

    return {
        static_cast<int>(std::round(cropCenterX - unscaledCropW / 2.0)),
        static_cast<int>(std::round(cropCenterY - unscaledCropH / 2.0)),
        static_cast<int>(std::round(unscaledCropW)),
        static_cast<int>(std::round(unscaledCropH)),
    };
}

CropSize applyCropTemplate(double ratio, double currentCropW, double currentCropH,
                            double imgW, double imgH) {
    if (imgW / imgH > ratio) {
        double h = imgH;
        double w = h * ratio;
        return { w, h };
    }
    double w = imgW;
    double h = w / ratio;
    return { w, h };
}

CropSize resizeCropFromHandle(DragHandle handle, double deltaX, double deltaY,
                               double currentCropW, double currentCropH,
                               std::optional<double> aspectRatio) {
    double w = currentCropW;
    double h = currentCropH;

    switch (handle) {
        case DragHandle::NW:  w -= deltaX * 2; h -= deltaY * 2; break;
        case DragHandle::N:   h -= deltaY * 2; break;
        case DragHandle::NE:  w += deltaX * 2; h -= deltaY * 2; break;
        case DragHandle::W:   w -= deltaX * 2; break;
        case DragHandle::E:   w += deltaX * 2; break;
        case DragHandle::SW:  w -= deltaX * 2; h += deltaY * 2; break;
        case DragHandle::S:   h += deltaY * 2; break;
        case DragHandle::SE:  w += deltaX * 2; h += deltaY * 2; break;
        case DragHandle::Move:
            return { currentCropW, currentCropH };
    }

    w = std::max(w, crop_constants::kMinCropSize);
    h = std::max(h, crop_constants::kMinCropSize);

    if (aspectRatio.has_value()) {
        double r = *aspectRatio;
        bool isHorizontal = (handle == DragHandle::W || handle == DragHandle::E);
        if (isHorizontal) {
            h = w / r;
        } else {
            w = h * r;
        }
    }

    return { w, h };
}

bool isCropSizeValid(double newCropW, double newCropH,
                      double imgW, double imgH,
                      double scale, double angleRad,
                      double perspVRad, double perspHRad) {
    double minS = computeMinScaleWithPerspective(newCropW, newCropH, imgW, imgH,
                                                  angleRad, perspVRad, perspHRad);
    return scale >= minS;
}

std::optional<DragHandle> hitTestHandles(double px, double py,
                                          double cropW, double cropH,
                                          double hitRadius) {
    double hw = cropW / 2.0;
    double hh = cropH / 2.0;

    struct HandlePos { DragHandle handle; double x; double y; };
    HandlePos handles[] = {
        { DragHandle::NW, -hw, -hh },
        { DragHandle::N,    0, -hh },
        { DragHandle::NE,  hw, -hh },
        { DragHandle::W,  -hw,   0 },
        { DragHandle::E,   hw,   0 },
        { DragHandle::SW, -hw,  hh },
        { DragHandle::S,    0,  hh },
        { DragHandle::SE,  hw,  hh },
    };

    for (const auto& h : handles) {
        double dx = px - h.x;
        double dy = py - h.y;
        if (dx * dx + dy * dy <= hitRadius * hitRadius) {
            return h.handle;
        }
    }

    // Inside crop rect = move
    if (std::abs(px) <= hw && std::abs(py) <= hh) {
        return DragHandle::Move;
    }

    return std::nullopt;
}

} // namespace pivox
