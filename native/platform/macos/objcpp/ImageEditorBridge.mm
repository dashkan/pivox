#import "ImageEditorBridge.h"
#include "image_editor_engine.h"

// ─────────────────────────────────────────────────────────────────────
// IEBCropTemplate
// ─────────────────────────────────────────────────────────────────────

@implementation IEBCropTemplate
- (instancetype)initWithLabel:(NSString*)label ratio:(double)ratio {
  self = [super init];
  if (self) {
    _label = [label copy];
    _ratio = ratio;
    _isFreeform = (ratio <= 0);
  }
  return self;
}
+ (instancetype)freeform {
  return [[IEBCropTemplate alloc] initWithLabel:@"Free" ratio:0];
}
@end

// ─────────────────────────────────────────────────────────────────────
// IEBCropRect
// ─────────────────────────────────────────────────────────────────────

@implementation IEBCropRect
@end

// ─────────────────────────────────────────────────────────────────────
// IEBState
// ─────────────────────────────────────────────────────────────────────

@implementation IEBState
@end

// ─────────────────────────────────────────────────────────────────────
// ImageEditorBridge
// ─────────────────────────────────────────────────────────────────────

@implementation ImageEditorBridge {
  std::unique_ptr<pivox::ImageEditorEngine> _engine;
}

- (instancetype)init {
  self = [super init];
  if (self) {
    pivox::ImageEditorEngine::Options opts;
    opts.templates = {
        {"16:9", 16.0 / 9.0}, {"4:3", 4.0 / 3.0}, {"1:1", 1.0},
        {"9:16", 9.0 / 16.0}, {"3:4", 3.0 / 4.0}, {"21:9", 21.0 / 9.0},
        {"2.39:1", 2.39},     {"1.85:1", 1.85},   {"2:1", 2.0},
    };
    _engine = std::make_unique<pivox::ImageEditorEngine>(opts);

    __weak typeof(self) weakSelf = self;
    _engine->onChange([weakSelf](const pivox::ImageEditorState&) {
      if (auto strong = weakSelf) {
        if (strong.onStateChanged) {
          strong.onStateChanged();
        }
      }
    });
  }
  return self;
}

// ── Image ────────────────────────────────────────────────────────────

- (void)setImageLoadedWidth:(int)width height:(int)height {
  _engine->setImageLoaded(width, height);
}

- (void)setImageError:(NSString*)error {
  _engine->setImageError(error.UTF8String);
}

// ── Container ────────────────────────────────────────────────────────

- (void)setContainerWidth:(double)width height:(double)height {
  _engine->setContainerSize(width, height);
}

- (void)setViewportScale:(double)scale {
  _engine->setViewportScale(scale);
}

// ── Actions ──────────────────────────────────────────────────────────

- (void)rotateClockwise {
  _engine->rotateClockwise();
}
- (void)rotateCounterClockwise {
  _engine->rotateCounterClockwise();
}
- (void)toggleFlipHorizontal {
  _engine->toggleFlipHorizontal();
}
- (void)toggleFlipVertical {
  _engine->toggleFlipVertical();
}

- (void)applyTemplateWithLabel:(NSString*)label ratio:(double)ratio {
  pivox::CropTemplate tmpl{label.UTF8String, ratio};
  _engine->applyTemplate(tmpl);
}

- (void)applyFreeformTemplate {
  pivox::CropTemplate tmpl{"Free", std::nullopt};
  _engine->applyTemplate(tmpl);
}

- (void)setStraighten:(double)degrees {
  _engine->setStraighten(degrees);
}
- (void)commitStraighten {
  _engine->commitStraighten();
}
- (void)setPerspectiveV:(double)degrees {
  _engine->setPerspectiveV(degrees);
}
- (void)setPerspectiveH:(double)degrees {
  _engine->setPerspectiveH(degrees);
}
- (void)commitPerspective {
  _engine->commitPerspective();
}
- (void)reset {
  _engine->reset();
}
- (void)undo {
  _engine->undo();
}
- (void)redo {
  _engine->redo();
}
- (void)zoomIn {
  _engine->zoomIn();
}
- (void)zoomOut {
  _engine->zoomOut();
}
- (void)zoomToFit {
  _engine->zoomToFit();
}
- (void)setZoom:(double)level {
  _engine->setZoom(level);
}
- (void)enterCropMode {
  _engine->enterCropMode();
}
- (void)exitCropMode {
  _engine->exitCropMode();
}

// ── Pointer input ────────────────────────────────────────────────────

- (void)onPointerDownX:(double)x y:(double)y altOrMiddle:(BOOL)alt {
  _engine->onPointerDown(x, y, alt);
}

- (void)onPointerMoveX:(double)x
                     y:(double)y
          screenDeltaX:(double)sdx
          screenDeltaY:(double)sdy {
  _engine->onPointerMove(x, y, sdx, sdy);
}

- (void)onPointerUp {
  _engine->onPointerUp();
}

// ── Hit test ─────────────────────────────────────────────────────────

- (nullable NSString*)hitTestX:(double)x y:(double)y {
  auto result = _engine->hitTest(x, y);
  if (!result.has_value()) return nil;
  switch (*result) {
    case pivox::DragHandle::NW:
      return @"nw";
    case pivox::DragHandle::N:
      return @"n";
    case pivox::DragHandle::NE:
      return @"ne";
    case pivox::DragHandle::W:
      return @"w";
    case pivox::DragHandle::E:
      return @"e";
    case pivox::DragHandle::SW:
      return @"sw";
    case pivox::DragHandle::S:
      return @"s";
    case pivox::DragHandle::SE:
      return @"se";
    case pivox::DragHandle::Move:
      return @"move";
  }
}

// ── State snapshot ───────────────────────────────────────────────────

- (IEBState*)currentState {
  auto& s = _engine->state();
  auto* state = [[IEBState alloc] init];
  state.cropWidth = s.cropWidth;
  state.cropHeight = s.cropHeight;
  state.rotation = s.rotation;
  state.straighten = s.straighten;
  state.perspectiveV = s.perspectiveV;
  state.perspectiveH = s.perspectiveH;
  state.scale = s.scale;
  state.tx = s.tx;
  state.ty = s.ty;
  state.flipHorizontal = s.flipHorizontal;
  state.flipVertical = s.flipVertical;
  state.naturalWidth = s.naturalWidth;
  state.naturalHeight = s.naturalHeight;
  state.isDragging = s.isDragging;
  state.canUndo = s.canUndo;
  state.canRedo = s.canRedo;
  state.isDirty = s.isDirty;
  state.zoom = s.zoom;
  state.isZoomFit = (s.zoomMode == pivox::ZoomMode::Fit);
  state.isCropMode = (s.mode == pivox::EditorMode::Crop);
  state.panOffsetX = s.panOffset.x;
  state.panOffsetY = s.panOffset.y;

  if (s.activeTemplate.has_value()) {
    auto& t = *s.activeTemplate;
    state.activeTemplate =
        t.ratio.has_value()
            ? [[IEBCropTemplate alloc] initWithLabel:@(t.label.c_str())
                                               ratio:*t.ratio]
            : [IEBCropTemplate freeform];
  }

  NSMutableArray* templates =
      [NSMutableArray arrayWithCapacity:s.templates.size()];
  for (auto& t : s.templates) {
    [templates addObject:t.ratio.has_value()
                             ? [[IEBCropTemplate alloc]
                                   initWithLabel:@(t.label.c_str())
                                           ratio:*t.ratio]
                             : [IEBCropTemplate freeform]];
  }
  state.templates = templates;

  return state;
}

// ── Crop result ──────────────────────────────────────────────────────

- (IEBCropRect*)getCropRect {
  auto r = _engine->getCropRect();
  auto* rect = [[IEBCropRect alloc] init];
  rect.x = r.x;
  rect.y = r.y;
  rect.width = r.width;
  rect.height = r.height;
  return rect;
}

@end
