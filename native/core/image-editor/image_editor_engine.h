#pragma once

#include "image_editor_types.h"
#include "crop_math.h"
#include <vector>
#include <functional>

namespace pivox {

/// Options for constructing an ImageEditorEngine.
struct ImageEditorEngineOptions {
    std::vector<CropTemplate> templates;
    std::optional<CropTemplate> defaultTemplate;
    int maxHistory = 50;
};

/// Image editor engine — manages crop state, undo/redo, drag interactions.
/// Platform-agnostic: no rendering, no DOM, no platform APIs.
/// The platform layer reads state() and renders accordingly.
class ImageEditorEngine {
public:
    using Options = ImageEditorEngineOptions;

    explicit ImageEditorEngine(const Options& options = {});

    // ── Image ────────────────────────────────────────────────────────

    /// Called when the platform finishes loading the image.
    void setImageLoaded(int naturalWidth, int naturalHeight);

    /// Called when image loading fails.
    void setImageError(const std::string& error);

    // ── Container ────────────────────────────────────────────────────

    /// Called when the platform container resizes (for viewport scale).
    void setContainerSize(double width, double height);

    // ── Actions ──────────────────────────────────────────────────────

    void rotateClockwise();
    void rotateCounterClockwise();
    void toggleFlipHorizontal();
    void toggleFlipVertical();
    void applyTemplate(const CropTemplate& tmpl);
    void setStraighten(double degrees);
    void commitStraighten();
    void setPerspectiveV(double degrees);
    void setPerspectiveH(double degrees);
    void commitPerspective();
    void setResizeMode(ResizeMode mode);
    void reset();
    void undo();
    void redo();

    void zoomIn();
    void zoomOut();
    void zoomToFit();
    void setZoom(double level);

    void enterCropMode();
    void exitCropMode();

    // ── Pointer input ────────────────────────────────────────────────
    // Coordinates are in crop-centered space (0,0 = crop center),
    // already converted from screen space by the platform renderer.

    void onPointerDown(double cropX, double cropY, bool isAltOrMiddle = false);
    void onPointerMove(double cropX, double cropY, double screenDeltaX = 0, double screenDeltaY = 0);
    void onPointerUp();

    // ── State output ─────────────────────────────────────────────────

    const ImageEditorState& state() const { return state_; }
    CropRect getCropRect() const;

    /// Returns which handle is under the given crop-space point (for cursor).
    std::optional<DragHandle> hitTest(double cropX, double cropY) const;

    // ── Change notification ──────────────────────────────────────────

    using OnChangeCallback = std::function<void(const ImageEditorState&)>;
    void onChange(OnChangeCallback callback) { onChangeCallback_ = std::move(callback); }

    // ── Viewport info (set by platform, read by drag handlers) ───────

    void setViewportScale(double scale) { viewportScale_ = scale; }
    double viewportScale() const { return viewportScale_; }

private:
    void updateState(const ImageEditorState& newState);
    void pushHistoryAndUpdate(const ImageEditorState& newState);
    void applyRotationChange(int rotation, double straighten, bool pushToHistory);
    void applyPerspectiveChange(double perspV, double perspH, bool pushToHistory);

    static EditState extractEditState(const ImageEditorState& s);
    static bool isEditStateDirty(const EditState& a, const EditState& b);

    ImageEditorState state_;
    EditState initialEditState_;

    // Undo history
    std::vector<EditState> historyPast_;
    std::vector<EditState> historyFuture_;
    int maxHistory_;

    // Drag state
    struct DragOrigin {
        DragHandle handle;
        double screenX;
        double screenY;
        EditState originalEditState;
    };
    std::optional<DragOrigin> dragOrigin_;
    std::optional<EditState> preDragEditState_;
    std::optional<EditState> preStraightenEditState_;
    std::optional<EditState> prePerspectiveEditState_;

    // Viewport pan origin
    struct PanOrigin {
        double pointerX;
        double pointerY;
        double originalOffsetX;
        double originalOffsetY;
    };
    std::optional<PanOrigin> panOrigin_;

    double viewportScale_ = 1.0;
    double containerWidth_ = 0;
    double containerHeight_ = 0;

    OnChangeCallback onChangeCallback_;
};

} // namespace pivox
