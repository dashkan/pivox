#include "image_editor_engine.h"

#include <algorithm>
#include <cmath>

namespace pivox {

static const CropTemplate kFreeTemplate{"Free", std::nullopt};

// ─────────────────────────────────────────────────────────────────────
// Construction
// ─────────────────────────────────────────────────────────────────────

ImageEditorEngine::ImageEditorEngine(const Options& options)
    : maxHistory_(options.maxHistory) {
  // Build template list: Free first, then user templates.
  std::vector<CropTemplate> allTemplates;
  allTemplates.push_back(kFreeTemplate);
  for (auto& t : options.templates) {
    if (t.label != kFreeTemplate.label) {
      allTemplates.push_back(t);
    }
  }

  auto defaultTemplate = options.defaultTemplate.value_or(kFreeTemplate);

  initialEditState_ = EditState{};
  initialEditState_.activeTemplate = defaultTemplate;
  initialEditState_.resizeMode = ResizeMode::Crop;

  state_ = ImageEditorState{};
  static_cast<EditState&>(state_) = initialEditState_;
  state_.templates = std::move(allTemplates);
  state_.mode = EditorMode::View;
  state_.zoom = 100;
  state_.zoomMode = ZoomMode::Fit;
}

// ─────────────────────────────────────────────────────────────────────
// Image loading
// ─────────────────────────────────────────────────────────────────────

void ImageEditorEngine::setImageLoaded(int naturalWidth, int naturalHeight) {
  EditState editState{};
  editState.cropWidth = naturalWidth;
  editState.cropHeight = naturalHeight;
  editState.scale = 1.0;
  editState.activeTemplate = state_.activeTemplate;
  editState.resizeMode = ResizeMode::Crop;

  initialEditState_ = editState;
  historyPast_.clear();
  historyFuture_.clear();

  static_cast<EditState&>(state_) = editState;
  state_.imageStatus = ImageEditorState::ImageStatus::Loaded;
  state_.imageError.clear();
  state_.naturalWidth = naturalWidth;
  state_.naturalHeight = naturalHeight;
  state_.canUndo = false;
  state_.canRedo = false;
  state_.isDirty = false;
  state_.isDragging = false;
  state_.activeHandle = std::nullopt;
  state_.zoom = 100;
  state_.zoomMode = ZoomMode::Fit;
  state_.panOffset = {0, 0};
  state_.isPanning = false;
  state_.mode = EditorMode::View;

  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::setImageError(const std::string& error) {
  state_.imageStatus = ImageEditorState::ImageStatus::Error;
  state_.imageError = error;
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

// ─────────────────────────────────────────────────────────────────────
// Container
// ─────────────────────────────────────────────────────────────────────

void ImageEditorEngine::setContainerSize(double width, double height) {
  containerWidth_ = width;
  containerHeight_ = height;
}

// ─────────────────────────────────────────────────────────────────────
// Actions
// ─────────────────────────────────────────────────────────────────────

void ImageEditorEngine::rotateClockwise() {
  int rotation = (state_.rotation + 90) % 360;
  applyRotationChange(rotation, state_.straighten, true);
}

void ImageEditorEngine::rotateCounterClockwise() {
  int rotation = (state_.rotation + 270) % 360;
  applyRotationChange(rotation, state_.straighten, true);
}

void ImageEditorEngine::toggleFlipHorizontal() {
  auto newState = state_;
  newState.flipHorizontal = !state_.flipHorizontal;
  pushHistoryAndUpdate(newState);
}

void ImageEditorEngine::toggleFlipVertical() {
  auto newState = state_;
  newState.flipVertical = !state_.flipVertical;
  pushHistoryAndUpdate(newState);
}

void ImageEditorEngine::setStraighten(double degrees) {
  double clamped = std::max(-45.0, std::min(45.0, degrees));
  if (!preStraightenEditState_.has_value()) {
    preStraightenEditState_ = extractEditState(state_);
  }
  applyRotationChange(state_.rotation, clamped, false);
}

void ImageEditorEngine::commitStraighten() {
  if (!preStraightenEditState_.has_value()) { return;
}
  auto pre = *preStraightenEditState_;
  preStraightenEditState_ = std::nullopt;

  // Push the pre-straighten state to history.
  historyPast_.push_back(pre);
  if (static_cast<int>(historyPast_.size()) > maxHistory_) {
    historyPast_.erase(historyPast_.begin());
  }
  historyFuture_.clear();

  state_.canUndo = true;
  state_.canRedo = false;
  state_.isDirty =
      isEditStateDirty(extractEditState(state_), initialEditState_);
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::setPerspectiveV(double degrees) {
  double clamped = std::max(-30.0, std::min(30.0, degrees));
  if (!prePerspectiveEditState_.has_value()) {
    prePerspectiveEditState_ = extractEditState(state_);
  }
  applyPerspectiveChange(clamped, state_.perspectiveH, false);
}

void ImageEditorEngine::setPerspectiveH(double degrees) {
  double clamped = std::max(-30.0, std::min(30.0, degrees));
  if (!prePerspectiveEditState_.has_value()) {
    prePerspectiveEditState_ = extractEditState(state_);
  }
  applyPerspectiveChange(state_.perspectiveV, clamped, false);
}

void ImageEditorEngine::commitPerspective() {
  if (!prePerspectiveEditState_.has_value()) { return;
}
  auto pre = *prePerspectiveEditState_;
  prePerspectiveEditState_ = std::nullopt;

  historyPast_.push_back(pre);
  if (static_cast<int>(historyPast_.size()) > maxHistory_) {
    historyPast_.erase(historyPast_.begin());
  }
  historyFuture_.clear();

  state_.canUndo = true;
  state_.canRedo = false;
  state_.isDirty =
      isEditStateDirty(extractEditState(state_), initialEditState_);
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::applyTemplate(const CropTemplate& tmpl) {
  auto newState = state_;
  newState.activeTemplate = tmpl;

  if (!tmpl.ratio.has_value()) {
    // Freeform — just update the template, keep dimensions.
    pushHistoryAndUpdate(newState);
    return;
  }

  auto [cropW, cropH] =
      applyCropTemplate(*tmpl.ratio, state_.cropWidth, state_.cropHeight,
                        state_.naturalWidth, state_.naturalHeight);

  double angle = totalAngleRad(state_.rotation, state_.straighten);
  double pv = state_.perspectiveV * M_PI / 180.0;
  double ph = state_.perspectiveH * M_PI / 180.0;
  double minScale = computeMinScaleWithPerspective(
      cropW, cropH, state_.naturalWidth, state_.naturalHeight, angle, pv, ph);
  double scale = std::max(state_.scale, minScale);

  auto [maxTx, maxTy] =
      computeTranslationBounds(cropW, cropH, state_.naturalWidth,
                               state_.naturalHeight, scale, angle, pv, ph);
  auto [tx, ty] = clampTranslation(state_.tx, state_.ty, maxTx, maxTy);

  newState.cropWidth = cropW;
  newState.cropHeight = cropH;
  newState.scale = scale;
  newState.tx = tx;
  newState.ty = ty;
  pushHistoryAndUpdate(newState);
}

void ImageEditorEngine::setResizeMode(ResizeMode mode) {
  auto newState = state_;
  newState.resizeMode = mode;
  pushHistoryAndUpdate(newState);
}

void ImageEditorEngine::reset() {
  auto newState = state_;
  static_cast<EditState&>(newState) = initialEditState_;
  pushHistoryAndUpdate(newState);
}

void ImageEditorEngine::undo() {
  if (historyPast_.empty()) { return;
}

  auto previous = historyPast_.back();
  historyPast_.pop_back();
  historyFuture_.insert(historyFuture_.begin(), extractEditState(state_));

  static_cast<EditState&>(state_) = previous;
  state_.canUndo = !historyPast_.empty();
  state_.canRedo = true;
  state_.isDirty = isEditStateDirty(previous, initialEditState_);
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::redo() {
  if (historyFuture_.empty()) { return;
}

  auto next = historyFuture_.front();
  historyFuture_.erase(historyFuture_.begin());
  historyPast_.push_back(extractEditState(state_));

  static_cast<EditState&>(state_) = next;
  state_.canUndo = true;
  state_.canRedo = !historyFuture_.empty();
  state_.isDirty = isEditStateDirty(next, initialEditState_);
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::zoomIn() {
  state_.zoom = std::min(zoom_constants::kZoomMax,
                         state_.zoom + zoom_constants::kZoomStep);
  state_.zoomMode = ZoomMode::Manual;
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::zoomOut() {
  state_.zoom = std::max(zoom_constants::kZoomMin,
                         state_.zoom - zoom_constants::kZoomStep);
  state_.zoomMode = ZoomMode::Manual;
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::zoomToFit() {
  state_.zoom = 100;
  state_.zoomMode = ZoomMode::Fit;
  state_.panOffset = {0, 0};
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::setZoom(double level) {
  state_.zoom = std::max(zoom_constants::kZoomMin,
                         std::min(zoom_constants::kZoomMax, level));
  state_.zoomMode = ZoomMode::Manual;
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::enterCropMode() {
  state_.mode = EditorMode::Crop;
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::exitCropMode() {
  state_.mode = EditorMode::View;
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

// ─────────────────────────────────────────────────────────────────────
// Pointer input
// ─────────────────────────────────────────────────────────────────────

void ImageEditorEngine::onPointerDown(double cropX, double cropY,
                                      bool isAltOrMiddle) {
  if (state_.imageStatus != ImageEditorState::ImageStatus::Loaded) { return;
}
  if (state_.mode != EditorMode::Crop) { return;
}

  // Alt+click or middle button → viewport pan when zoomed.
  if (isAltOrMiddle && state_.zoomMode == ZoomMode::Manual &&
      state_.zoom > 100) {
    panOrigin_ =
        PanOrigin{cropX, cropY, state_.panOffset.x, state_.panOffset.y};
    state_.isPanning = true;
    if (onChangeCallback_) { onChangeCallback_(state_);
}
    return;
  }

  double hitRadius = render_constants::kHandleHitRadius / viewportScale_;
  auto handle = hitTestHandles(cropX, cropY, state_.cropWidth,
                               state_.cropHeight, hitRadius);

  if (handle.has_value()) {
    preDragEditState_ = extractEditState(state_);
    dragOrigin_ = DragOrigin{*handle, cropX, cropY, extractEditState(state_)};
    state_.isDragging = true;
    state_.activeHandle = handle;
    if (onChangeCallback_) { onChangeCallback_(state_);
}
  }
}

void ImageEditorEngine::onPointerMove(double cropX, double cropY,
                                      double screenDeltaX,
                                      double screenDeltaY) {
  // Viewport pan
  if (state_.isPanning && panOrigin_.has_value()) {
    state_.panOffset.x = panOrigin_->originalOffsetX + screenDeltaX;
    state_.panOffset.y = panOrigin_->originalOffsetY + screenDeltaY;
    if (onChangeCallback_) { onChangeCallback_(state_);
}
    return;
  }

  // Drag (move image or resize crop)
  if (dragOrigin_.has_value()) {
    double dx = (cropX - dragOrigin_->screenX);
    double dy = (cropY - dragOrigin_->screenY);
    auto& orig = dragOrigin_->originalEditState;
    double angle = totalAngleRad(state_.rotation, state_.straighten);
    double pvr = state_.perspectiveV * M_PI / 180.0;
    double phr = state_.perspectiveH * M_PI / 180.0;

    if (dragOrigin_->handle == DragHandle::Move) {
      double newTx = orig.tx + dx;
      double newTy = orig.ty + dy;
      auto [maxTx, maxTy] = computeTranslationBounds(
          state_.cropWidth, state_.cropHeight, state_.naturalWidth,
          state_.naturalHeight, state_.scale, angle, pvr, phr);
      auto [clampedTx, clampedTy] =
          clampTranslation(newTx, newTy, maxTx, maxTy);
      state_.tx = clampedTx;
      state_.ty = clampedTy;
    } else {
      auto [newCropW, newCropH] = resizeCropFromHandle(
          dragOrigin_->handle, dx, dy, orig.cropWidth, orig.cropHeight,
          orig.activeTemplate ? orig.activeTemplate->ratio : std::nullopt);

      double iw = state_.naturalWidth;
      double ih = state_.naturalHeight;
      newCropW = std::min(newCropW, static_cast<double>(iw));
      newCropH = std::min(newCropH, static_cast<double>(ih));

      auto ratio =
          orig.activeTemplate ? orig.activeTemplate->ratio : std::nullopt;
      if (ratio.has_value()) {
        if (newCropW / newCropH > *ratio) {
          newCropW = newCropH * *ratio;
        } else {
          newCropH = newCropW / *ratio;
        }
      }

      double newMinScale = computeMinScaleWithPerspective(
          newCropW, newCropH, iw, ih, angle, pvr, phr);
      double scale = std::max(
          newMinScale, state_.scale > orig.scale ? state_.scale : newMinScale);

      auto [maxTx, maxTy] = computeTranslationBounds(newCropW, newCropH, iw, ih,
                                                     scale, angle, pvr, phr);
      auto [clampedTx, clampedTy] =
          clampTranslation(state_.tx, state_.ty, maxTx, maxTy);

      state_.cropWidth = newCropW;
      state_.cropHeight = newCropH;
      state_.scale = scale;
      state_.tx = clampedTx;
      state_.ty = clampedTy;
    }

    if (onChangeCallback_) { onChangeCallback_(state_);
}
  }
}

void ImageEditorEngine::onPointerUp() {
  if (state_.isPanning) {
    panOrigin_ = std::nullopt;
    state_.isPanning = false;
    if (onChangeCallback_) { onChangeCallback_(state_);
}
    return;
  }

  if (dragOrigin_.has_value()) {
    auto preDrag = preDragEditState_.value_or(initialEditState_);
    dragOrigin_ = std::nullopt;
    preDragEditState_ = std::nullopt;

    historyPast_.push_back(preDrag);
    if (static_cast<int>(historyPast_.size()) > maxHistory_) {
      historyPast_.erase(historyPast_.begin());
    }
    historyFuture_.clear();

    state_.isDragging = false;
    state_.activeHandle = std::nullopt;
    state_.canUndo = true;
    state_.canRedo = false;
    state_.isDirty =
        isEditStateDirty(extractEditState(state_), initialEditState_);
    if (onChangeCallback_) { onChangeCallback_(state_);
}
  }
}

// ─────────────────────────────────────────────────────────────────────
// State output
// ─────────────────────────────────────────────────────────────────────

CropRect ImageEditorEngine::getCropRect() const {
  double angle = totalAngleRad(state_.rotation, state_.straighten);
  return stateToImageCropRect(state_.cropWidth, state_.cropHeight,
                              state_.naturalWidth, state_.naturalHeight,
                              state_.tx, state_.ty, state_.scale, angle);
}

std::optional<DragHandle> ImageEditorEngine::hitTest(double cropX,
                                                     double cropY) const {
  double hitRadius = render_constants::kHandleHitRadius / viewportScale_;
  return hitTestHandles(cropX, cropY, state_.cropWidth, state_.cropHeight,
                        hitRadius);
}

// ─────────────────────────────────────────────────────────────────────
// Internal — rotation with auto-scale and clamp
// ─────────────────────────────────────────────────────────────────────

void ImageEditorEngine::applyRotationChange(int rotation, double straighten,
                                            bool pushToHistory) {
  double angle = totalAngleRad(rotation, straighten);
  double iw = state_.naturalWidth;
  double ih = state_.naturalHeight;
  double cw = state_.cropWidth;
  double ch = state_.cropHeight;

  double pvr = state_.perspectiveV * M_PI / 180.0;
  double phr = state_.perspectiveH * M_PI / 180.0;
  double minScale =
      computeMinScaleWithPerspective(cw, ch, iw, ih, angle, pvr, phr);
  double scale = std::max(state_.scale, minScale);

  auto [maxTx, maxTy] =
      computeTranslationBounds(cw, ch, iw, ih, scale, angle, pvr, phr);
  auto [tx, ty] = clampTranslation(state_.tx, state_.ty, maxTx, maxTy);

  auto newState = state_;
  newState.rotation = rotation;
  newState.straighten = straighten;
  newState.scale = scale;
  newState.tx = tx;
  newState.ty = ty;

  if (pushToHistory) {
    pushHistoryAndUpdate(newState);
  } else {
    updateState(newState);
  }
}

void ImageEditorEngine::applyPerspectiveChange(double perspV, double perspH,
                                               bool pushToHistory) {
  double angle = totalAngleRad(state_.rotation, state_.straighten);
  double iw = state_.naturalWidth;
  double ih = state_.naturalHeight;
  double cw = state_.cropWidth;
  double ch = state_.cropHeight;

  double pvr = perspV * M_PI / 180.0;
  double phr = perspH * M_PI / 180.0;
  double minScale =
      computeMinScaleWithPerspective(cw, ch, iw, ih, angle, pvr, phr);
  double scale = std::max(state_.scale, minScale);

  auto [maxTx, maxTy] =
      computeTranslationBounds(cw, ch, iw, ih, scale, angle, pvr, phr);
  auto [tx, ty] = clampTranslation(state_.tx, state_.ty, maxTx, maxTy);

  auto newState = state_;
  newState.perspectiveV = perspV;
  newState.perspectiveH = perspH;
  newState.scale = scale;
  newState.tx = tx;
  newState.ty = ty;

  if (pushToHistory) {
    pushHistoryAndUpdate(newState);
  } else {
    updateState(newState);
  }
}

// ─────────────────────────────────────────────────────────────────────
// Internal — state management
// ─────────────────────────────────────────────────────────────────────

void ImageEditorEngine::updateState(const ImageEditorState& newState) {
  state_ = newState;
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

void ImageEditorEngine::pushHistoryAndUpdate(const ImageEditorState& newState) {
  auto currentEdit = extractEditState(state_);
  historyPast_.push_back(currentEdit);
  if (static_cast<int>(historyPast_.size()) > maxHistory_) {
    historyPast_.erase(historyPast_.begin());
  }
  historyFuture_.clear();

  auto newEdit = extractEditState(newState);
  state_ = newState;
  state_.canUndo = true;
  state_.canRedo = false;
  state_.isDirty = isEditStateDirty(newEdit, initialEditState_);
  if (onChangeCallback_) { onChangeCallback_(state_);
}
}

EditState ImageEditorEngine::extractEditState(const ImageEditorState& s) {
  return static_cast<const EditState&>(s);
}

bool ImageEditorEngine::isEditStateDirty(const EditState& a,
                                         const EditState& b) {
  return a != b;
}

}  // namespace pivox
