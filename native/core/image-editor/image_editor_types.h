#pragma once

#include <string>
#include <vector>
#include <functional>
#include <optional>
#include <cmath>

namespace pivox {

/// Pixel-space rectangle. Origin is top-left of the source image.
struct CropRect {
    int x = 0;
    int y = 0;
    int width = 0;
    int height = 0;
};

/// Resize strategy when crop doesn't match target dimensions.
enum class ResizeMode { Crop, Cover, Fit };

/// A named aspect-ratio preset. nullopt ratio means freeform.
struct CropTemplate {
    std::string label;
    std::optional<double> ratio;  // width / height, or nullopt for freeform
};

/// Which edge/corner the user is dragging, or Move for the whole rect.
enum class DragHandle { NW, N, NE, W, E, SW, S, SE, Move };

/// Zoom mode.
enum class ZoomMode { Fit, Manual };

/// Editor mode.
enum class EditorMode { View, Crop };

/// The editable parameters tracked in undo history.
/// Uses the "crop-as-viewport" model: the crop rect is a fixed window,
/// the image transforms (scale, rotate, translate) behind it.
struct EditState {
    double cropWidth = 0;
    double cropHeight = 0;
    int rotation = 0;           // 0, 90, 180, 270
    double straighten = 0;      // -45..45
    double perspectiveV = 0;    // vertical perspective correction, -30..30 degrees
    double perspectiveH = 0;    // horizontal perspective correction, -30..30 degrees
    double scale = 1;
    double tx = 0;              // image X translation (image-pixel units)
    double ty = 0;              // image Y translation (image-pixel units)
    bool flipHorizontal = false;
    bool flipVertical = false;
    std::optional<CropTemplate> activeTemplate;
    ResizeMode resizeMode = ResizeMode::Crop;

    bool operator==(const EditState& other) const;
    bool operator!=(const EditState& other) const { return !(*this == other); }
};

/// Full state including UI metadata (not all tracked in undo).
struct ImageEditorState : EditState {
    std::string src;
    enum class ImageStatus { Idle, Loading, Loaded, Error };
    ImageStatus imageStatus = ImageStatus::Idle;
    std::string imageError;
    int naturalWidth = 0;
    int naturalHeight = 0;
    std::vector<CropTemplate> templates;
    bool isDragging = false;
    std::optional<DragHandle> activeHandle;
    bool canUndo = false;
    bool canRedo = false;
    bool isDirty = false;
    double zoom = 100;
    ZoomMode zoomMode = ZoomMode::Fit;
    struct { double x = 0; double y = 0; } panOffset;
    bool isPanning = false;
    EditorMode mode = EditorMode::View;
};

/// Shared rendering constants for visual consistency across platforms.
namespace render_constants {
    constexpr double kHandleRadius = 5.0;
    constexpr double kHandleHitRadius = 16.0;
    constexpr double kCropBorderWidth = 1.5;
    constexpr double kGridLineWidth = 0.5;
    constexpr int kGridDivisions = 3;
    constexpr double kOverlayOpacity = 0.5;
    constexpr double kCanvasPadding = 16.0;
}

/// Zoom constants.
namespace zoom_constants {
    constexpr double kZoomStep = 25.0;
    constexpr double kZoomMin = 10.0;
    constexpr double kZoomMax = 800.0;
}

/// Crop constraints.
namespace crop_constants {
    constexpr double kMinCropSize = 10.0;
    constexpr int kMaxHistory = 50;
}

/// Perspective correction constants.
/// Focal length multiplier: smaller = more dramatic effect.
/// Photos.app uses different intensities for each axis.
namespace perspective_constants {
    constexpr double kFocalLengthMultiplierV = 1.4;  // vertical: subtler
    constexpr double kFocalLengthMultiplierH = 0.8;  // horizontal: more dramatic
}

} // namespace pivox
