#pragma once

#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

/// Represents crop template for Swift consumption.
@interface IEBCropTemplate : NSObject
@property(nonatomic, copy, nullable) NSString *label;
@property(nonatomic) double ratio;  // 0 means freeform
@property(nonatomic) BOOL isFreeform;
- (instancetype)initWithLabel:(NSString *)label ratio:(double)ratio;
+ (instancetype)freeform;
@end

/// Crop rectangle result.
@interface IEBCropRect : NSObject
@property(nonatomic) int x;
@property(nonatomic) int y;
@property(nonatomic) int width;
@property(nonatomic) int height;
@end

/// Snapshot of the engine state for SwiftUI rendering.
@interface IEBState : NSObject
@property(nonatomic) double cropWidth;
@property(nonatomic) double cropHeight;
@property(nonatomic) int rotation;
@property(nonatomic) double straighten;
@property(nonatomic) double perspectiveV;
@property(nonatomic) double perspectiveH;
@property(nonatomic) double scale;
@property(nonatomic) double tx;
@property(nonatomic) double ty;
@property(nonatomic) BOOL flipHorizontal;
@property(nonatomic) BOOL flipVertical;
@property(nonatomic) int naturalWidth;
@property(nonatomic) int naturalHeight;
@property(nonatomic) BOOL isDragging;
@property(nonatomic) BOOL canUndo;
@property(nonatomic) BOOL canRedo;
@property(nonatomic) BOOL isDirty;
@property(nonatomic) double zoom;
@property(nonatomic) BOOL isZoomFit;
@property(nonatomic) BOOL isCropMode;
@property(nonatomic) double panOffsetX;
@property(nonatomic) double panOffsetY;
@property(nonatomic, nullable) IEBCropTemplate *activeTemplate;
@property(nonatomic, copy, nullable) NSArray<IEBCropTemplate *> *templates;
@end

/// Obj-C++ bridge wrapping the C++ ImageEditorEngine.
@interface ImageEditorBridge : NSObject

- (instancetype)init;

/// Called by the platform when the image finishes loading.
- (void)setImageLoadedWidth:(int)width height:(int)height;
- (void)setImageError:(NSString *)error;

/// Container size for viewport calculations.
- (void)setContainerWidth:(double)width height:(double)height;
- (void)setViewportScale:(double)scale;

/// Actions.
- (void)rotateClockwise;
- (void)rotateCounterClockwise;
- (void)toggleFlipHorizontal;
- (void)toggleFlipVertical;
- (void)applyTemplateWithLabel:(NSString *)label ratio:(double)ratio;
- (void)applyFreeformTemplate;
- (void)setStraighten:(double)degrees;
- (void)commitStraighten;
- (void)setPerspectiveV:(double)degrees;
- (void)setPerspectiveH:(double)degrees;
- (void)commitPerspective;
- (void)reset;
- (void)undo;
- (void)redo;
- (void)zoomIn;
- (void)zoomOut;
- (void)zoomToFit;
- (void)setZoom:(double)level;
- (void)enterCropMode;
- (void)exitCropMode;

/// Pointer input (crop-centered coordinates).
- (void)onPointerDownX:(double)x y:(double)y altOrMiddle:(BOOL)alt;
- (void)onPointerMoveX:(double)x
                     y:(double)y
          screenDeltaX:(double)sdx
          screenDeltaY:(double)sdy;
- (void)onPointerUp;

/// Hit test (returns handle name string or nil).
- (nullable NSString *)hitTestX:(double)x y:(double)y;

/// State snapshot.
- (nullable IEBState *)currentState;

/// Crop result in image pixel coordinates.
- (nullable IEBCropRect *)getCropRect;

/// Change callback — called on every state change.
@property(nonatomic, copy, nullable) void (^onStateChanged)(void);

@end

NS_ASSUME_NONNULL_END
