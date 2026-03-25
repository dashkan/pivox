import { createFileRoute } from '@tanstack/react-router';
import { ImageEditorFeature } from '@pivox/features/image-editor';
import { ImageEditor, DEFAULT_CROP_TEMPLATES } from '@pivox/ui/image-editor';
import { TooltipProvider } from '@pivox/primitives/tooltip';

export const Route = createFileRoute('/_app/image-editor')({
  component: ImageEditorPage,
});

const SAMPLE_IMAGE =
  'https://images.unsplash.com/photo-1506744038136-46273834b3fb?w=1920&q=80';

function ImageEditorPage() {
  return (
    <TooltipProvider>
      <ImageEditorFeature
        src={SAMPLE_IMAGE}
        templates={DEFAULT_CROP_TEMPLATES}
        onChange={(editState) => {
          console.log('Edit state changed:', editState);
        }}
      >
        <div className="flex h-[calc(100vh-3.5rem)] flex-col">
          {/* Top toolbar */}
          <ImageEditor.Toolbar>
            <ImageEditor.CropButton />
            <ImageEditor.CloseCropButton />

            <div className="mx-1 h-5 w-px bg-border" />

            <ImageEditor.UndoRedoControls />
            <ImageEditor.ResetButton />

            <div className="mx-1 h-5 w-px bg-border" />

            <ImageEditor.RotateControls />

            <div className="mx-1 h-5 w-px bg-border" />

            <ImageEditor.FlipControls />

            <div className="ml-auto" />

            <ImageEditor.ZoomControls />
          </ImageEditor.Toolbar>

          {/* Main area: canvas + sidebar */}
          <div className="flex min-h-0 flex-1">
            <ImageEditor.Root className="min-h-0 flex-1">
              <ImageEditor.Canvas />
            </ImageEditor.Root>

            <ImageEditor.SidebarSlot className="w-56 overflow-y-auto">
              <div>
                <h3 className="mb-2 text-sm font-medium">Resize Mode</h3>
                <ImageEditor.ResizeModePicker />
              </div>

              <div>
                <h3 className="mb-2 text-sm font-medium">Aspect Ratio</h3>
                <ImageEditor.TemplatePicker />
              </div>
            </ImageEditor.SidebarSlot>
          </div>
        </div>
      </ImageEditorFeature>
    </TooltipProvider>
  );
}
