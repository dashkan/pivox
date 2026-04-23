import AppKit
import SwiftUI

// MARK: - Observable wrapper around ImageEditorBridge

@Observable
class ImageEditModel {
  let bridge: ImageEditorBridge
  private(set) var state: IEBState
  var isEditing = false
  var flipHCount = 0  // increments on each horizontal flip (for animation trigger)
  var flipVCount = 0  // increments on each vertical flip

  init() {
    bridge = ImageEditorBridge()
    state = bridge.currentState()!
    bridge.onStateChanged = { [weak self] in
      guard let self, let s = self.bridge.currentState() else { return }
      self.state = s
    }
  }

  func loadImage(_ image: NSImage) {
    guard let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
      return
    }
    bridge.setImageLoadedWidth(Int32(cgImage.width), height: Int32(cgImage.height))
  }

  func enterEditMode() {
    isEditing = true
    bridge.enterCropMode()
  }

  func exitEditMode() {
    isEditing = false
    bridge.exitCropMode()
  }
}

// MARK: - Main ImageEditView (Photos-style)

struct ImageEditView: View {
  let image: NSImage
  @Binding var isEditing: Bool
  @Binding var sidebarVisibility: NavigationSplitViewVisibility
  var onDone: ((IEBCropRect) -> Void)?
  var onCancel: (() -> Void)?

  @State private var model = ImageEditModel()

  var body: some View {
    ImageEditCanvasContainer(model: model, image: image)
      .overlay(alignment: .trailing) {
        // Right tool panel — overlays canvas, no extra width
        if model.isEditing {
          CropToolPanel(model: model)
            .frame(width: 260)
            .background(.background)
            .transition(.move(edge: .trailing))
        }
      }
      .animation(.easeInOut(duration: 0.25), value: model.isEditing)
      .toolbar {
        if model.isEditing {
          editingToolbar
        } else {
          viewingToolbar
        }
      }
      .onAppear {
        model.loadImage(image)
      }
  }

  // ── View Mode Toolbar ────────────────────────────────────────────
  // [← Back]   [- ●─── +]                              [Edit]

  @ToolbarContentBuilder
  private var viewingToolbar: some ToolbarContent {
    ToolbarItem(placement: .navigation) {
      Button(action: { onCancel?() }) {
        Image(systemName: "chevron.left")
          .pivoxIconToolbar()
      }
      .help("Back")
      .accessibilityIdentifier("edit-back")
    }

    ToolbarItem(placement: .automatic) {
      zoomControls
    }

    ToolbarItem(placement: .primaryAction) {
      Button("Edit") {
        withAnimation {
          model.enterEditMode()
          isEditing = true
          sidebarVisibility = .detailOnly
        }
      }
      .accessibilityIdentifier("edit-enter")
    }
  }

  // ── Edit Mode Toolbar ────────────────────────────────────────────
  // [Revert]   [- ●─── +]     [Crop]                   [Done]

  @ToolbarContentBuilder
  private var editingToolbar: some ToolbarContent {
    ToolbarItem(placement: .navigation) {
      Button("Revert to Original") {
        model.bridge.reset()
      }
      .disabled(!model.state.isDirty)
      .accessibilityIdentifier("edit-revert")
    }

    ToolbarItem(placement: .automatic) {
      zoomControls
    }

    ToolbarItem(placement: .principal) {
      HStack(spacing: 2) {
        toolTabButton(label: "Crop", icon: "crop", isSelected: true)
      }
    }

    ToolbarItem(placement: .primaryAction) {
      Button("Done") {
        let cropRect = model.bridge.getCropRect()!
        withAnimation {
          model.exitEditMode()
          isEditing = false
          sidebarVisibility = .automatic
        }
        onDone?(cropRect)
      }
      .buttonStyle(.borderedProminent)
      .tint(.yellow)
      .foregroundStyle(.black)
      .accessibilityIdentifier("edit-done")
    }
  }

  // ── Zoom controls ───────────────────────────────────────────────

  private var zoomControls: some View {
    HStack(spacing: 6) {
      Image(systemName: "minus")
        .font(.caption2)
        .foregroundStyle(.secondary)
        .onTapGesture { model.bridge.zoomOut() }

      Slider(
        value: Binding(
          get: { model.state.zoom },
          set: { model.bridge.setZoom($0) }
        ), in: 100...800
      )
      .frame(width: 100)
      .controlSize(.small)
      .tint(.secondary)

      Image(systemName: "plus")
        .font(.caption2)
        .foregroundStyle(.secondary)
        .onTapGesture { model.bridge.zoomIn() }
    }
    .accessibilityIdentifier("edit-zoom")
  }

  private func toolTabButton(label: String, icon: String, isSelected: Bool) -> some View {
    Button(action: {}) {
      Text(label)
        .font(.subheadline)
        .fontWeight(isSelected ? .semibold : .regular)
    }
    .buttonStyle(.borderless)
    .padding(.horizontal, 12)
    .padding(.vertical, 6)
    .background(
      isSelected ? AnyShapeStyle(.primary.opacity(0.15)) : AnyShapeStyle(.clear), in: Capsule())
  }
}

// MARK: - Crop Tool Panel (right side, Photos-style)

struct CropToolPanel: View {
  let model: ImageEditModel

  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      // Header
      Text("CROP")
        .font(.caption)
        .fontWeight(.semibold)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 20)
        .padding(.top, 16)
        .padding(.bottom, 12)

      // Straighten + Perspective — spacing matches Photos.app
      VStack(spacing: 6) {
        RulerSliderRow(
          label: "Straighten", icon: "circle.and.line.horizontal.fill",
          value: Binding(
            get: { model.state.straighten },
            set: { model.bridge.setStraighten($0) }
          ),
          range: -45...45,
          onCommit: { model.bridge.commitStraighten() },
          id: "edit-straighten")

        RulerSliderRow(
          label: "Vertical", icon: "trapezoid.and.line.vertical.fill",
          value: Binding(
            get: { model.state.perspectiveV },
            set: { model.bridge.setPerspectiveV($0) }
          ),
          range: -30...30,
          onCommit: { model.bridge.commitPerspective() },
          id: "edit-perspective-v")

        RulerSliderRow(
          label: "Horizontal", icon: "trapezoid.and.line.horizontal.fill",
          value: Binding(
            get: { model.state.perspectiveH },
            set: { model.bridge.setPerspectiveH($0) }
          ),
          range: -30...30,
          onCommit: { model.bridge.commitPerspective() },
          id: "edit-perspective-h")
      }

      Divider().padding(.horizontal, 16).padding(.vertical, 8)

      // Flip
      VStack(alignment: .leading, spacing: 2) {
        CropToolButton(
          icon: "arrow.left.and.right.righttriangle.left.righttriangle.right",
          label: "Flip Horizontally", id: "edit-flip-h"
        ) {
          model.bridge.toggleFlipHorizontal()
          model.flipHCount += 1
        }
        CropToolButton(
          icon: "arrow.up.and.down.righttriangle.up.righttriangle.down",
          label: "Flip Vertically", id: "edit-flip-v"
        ) {
          model.bridge.toggleFlipVertical()
          model.flipVCount += 1
        }
      }
      .padding(.horizontal, 20)
      .padding(.vertical, 4)

      Divider().padding(.horizontal, 16).padding(.vertical, 8)

      // Aspect ratio header
      CropToolButton(icon: "aspectratio", label: "Aspect", id: "edit-aspect") {}
        .padding(.horizontal, 20)
        .padding(.bottom, 4)

      if let templates = model.state.templates {
        VStack(alignment: .leading, spacing: 0) {
          ForEach(Array(templates.enumerated()), id: \.offset) { _, tmpl in
            let isActive = model.state.activeTemplate?.label == tmpl.label
            Button(action: {
              if tmpl.isFreeform {
                model.bridge.applyFreeformTemplate()
              } else {
                model.bridge.applyTemplate(withLabel: tmpl.label ?? "", ratio: tmpl.ratio)
              }
            }) {
              HStack {
                if isActive {
                  Image(systemName: "checkmark")
                    .font(.caption2)
                    .frame(width: 16)
                } else {
                  Spacer().frame(width: 16)
                }
                Text(tmpl.label ?? "Free")
                  .font(.subheadline)
                Spacer()
              }
              .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.vertical, 3)
            .padding(.horizontal, 20)
          }
        }
      }

      Spacer()

      // Bottom: Undo/Redo + Reset
      Divider().padding(.horizontal, 16)

      HStack {
        Button(action: { model.bridge.undo() }) {
          Image(systemName: "arrow.uturn.backward")
        }
        .disabled(!model.state.canUndo)
        .help("Undo")
        .accessibilityIdentifier("edit-undo")

        Button(action: { model.bridge.redo() }) {
          Image(systemName: "arrow.uturn.forward")
        }
        .disabled(!model.state.canRedo)
        .help("Redo")
        .accessibilityIdentifier("edit-redo")

        Spacer()

        Button("Reset") {
          model.bridge.reset()
        }
        .disabled(!model.state.isDirty)
        .accessibilityIdentifier("edit-reset")
      }
      .padding(.horizontal, 20)
      .padding(.vertical, 12)
    }
  }
}

/// Crop tool panel button — matches Photos icon style.
/// Uses proper Button for accessibility (AXButton role, VoiceOver, keyboard).
struct CropToolButton: View {
  let icon: String
  let label: String
  var id: String = ""
  let action: () -> Void

  var body: some View {
    Button(action: action) {
      HStack(spacing: 8) {
        Image(systemName: icon)
          .font(.callout)
          .foregroundStyle(.secondary)
          .frame(width: 18)
        Text(label)
          .font(.callout)
          .foregroundStyle(.secondary)
      }
      .frame(maxWidth: .infinity, alignment: .leading)
      .contentShape(Rectangle())
    }
    .buttonStyle(.plain)
    .accessibilityIdentifier(id)
  }
}
