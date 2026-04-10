# ActiveX Drag-Drop Strategy

## Problem

WinUI 3 XAML Islands hosted in an ActiveX control cannot use standard drag-drop mechanisms when the host process runs elevated or with `uiAccess=true` (e.g., iNEWS).

### WinUI native drag (`IDragOperation::StartAsync`)

Works non-elevated. Broken when elevated — the WinRT system returns S_OK but a null async operation. This is a known WinUI bug:

- **Issue:** [microsoft/microsoft-ui-xaml#7690](https://github.com/microsoft/microsoft-ui-xaml/issues/7690)
- **Internal bug:** 38743518
- **Root cause:** `IDragOperation::StartAsync` delegates to `CoreDragDropManager` (Windows system component). The system component refuses to start drag operations in elevated processes.
- **Note:** WinUI's native drag does NOT use OLE `DoDragDrop`. Confirmed via Detours hook — `DoDragDrop` is never called. The WinRT drag system uses `microsoft.ui.input.dragdrop.DragDropManager` which communicates with the OS through compositor IPC, not OLE.

### OLE `DoDragDrop`

Cannot work alongside WinUI 3 XAML Islands on the same thread. The WinUI compositor's `IInputPointerSource` intercepts all mouse input before it reaches the Win32 message queue. `DoDragDrop`'s modal message loop (`PeekMessage`/`GetMessage`) starves — it receives no `WM_MOUSEMOVE` or `WM_LBUTTONUP`. The loop freezes until external input breaks through.

Confirmed through builds 008-026: `GetKeyState` shows button pressed, `QueryContinueDrag` is called once, then the loop hangs. No cursor feedback, no drop target tracking.

Attempted workarounds (all failed):
- `PostMessage`/`SendMessage` from `PointerPressed` and `DragStarting`
- `ReleaseCapture` before `DoDragDrop`
- `GetAsyncKeyState` in `QueryContinueDrag`
- Synthetic `WM_LBUTTONDOWN` via `SendMessage` and `SendInput`
- `DragDetect` before `DoDragDrop`
- Subclassing island child HWND (receives zero Win32 messages)
- Helper sibling window with `SetFocus` to pull focus from island
- All fail because the compositor consumes mouse input at the `IInputPointerSource` level, not the Win32 message level

## Solution: Manual In-Process Drag-Drop

Timer-based polling + direct `IDropTarget` COM calls. Bypasses both the WinUI compositor and OLE's modal loop.

### How it works

1. `DragStarting` fires on WinUI element → cancel WinUI drag → build `IDataObject` → start 16ms `SetTimer` on host HWND
2. `WM_TIMER` handler each tick:
   - `GetCursorPos` + `GetAsyncKeyState(VK_LBUTTON)` — poll hardware state
   - `WindowFromPoint` → find HWND under cursor
   - Walk parent chain: `GetProp(hwnd, "OleDropTargetInterface")` → find registered `IDropTarget`
   - Call `IDropTarget::DragEnter/DragOver/DragLeave` directly (in-process COM vtable calls)
   - Update cursor via `SetSystemCursor` (normal `SetCursor` is overridden by compositor)
3. Button released → `IDropTarget::Drop` + `KillTimer`
4. Escape → `IDropTarget::DragLeave` + `KillTimer`
5. `SystemParametersInfoW(SPI_SETCURSORS)` restores default cursors on cleanup

### Limitations

- **In-process only.** `GetProp("OleDropTargetInterface")` returns a pointer in the target process's address space. Cross-process drops (e.g., to Notepad) won't work with this approach.
- **Same-process is sufficient** for iNEWS: the ActiveX plugin and iNEWS story panel share the same process. All iNEWS windows with registered `IDropTarget` are reachable.
- **Cursor feedback** uses `SetSystemCursor` which globally replaces the system cursor. Restored via `SPI_SETCURSORS` on cleanup. The compositor overrides normal `SetCursor` calls.

## Next: Popup Window Drag

The manual drag system currently works from the main XAML Islands panel (ATL control's `DragStarting` handler starts the timer on `m_hWnd`). Popup windows (e.g., launched via `OnLaunchWindow`) need the same capability — drag from a popup to an iNEWS story panel.

### Challenge

`ManualDragState` and the timer live on the ATL side (`ATLControl.cpp`). The popup is created in TestControls (`TestPanel.cpp`). The popup's `DragStarting` handler needs to signal the ATL side to start the manual drag.

### Options

1. **COM event** — Add a `OnDragRequested` event to `_IATLControlEvents` dispinterface. TestControls raises it via a method on the ATL control. ATL handles it by starting the manual drag. Clean COM architecture but requires IDL changes.

2. **WinRT event** — Add a custom event to TestPanel (e.g., `DragRequested`). The ATL side subscribes when it wires up the content. Popup raises the event on the TestPanel instance. Simpler than COM events but requires TestPanel IDL changes.

3. **Window property + PostMessage** — ATL sets `SetProp(m_hWnd, "PivoxDragOwner", ...)` on its HWND during `OnCreate`. Popup's `DragStarting` finds the HWND via `GetProp` on the desktop window and posts `WM_USER+100`. Quick and dirty, no IDL changes.

4. **Shared interface** — Define a simple `IDragService` interface in TestControls that ATL implements. Pass it to TestPanel at construction. Popup calls `IDragService::StartDrag(payload)`. Cleanest long-term but most setup.

### Current implementation

Option 3 (PostMessage bridge) for the prototype. Popup's `DragStarting` finds the ATL HWND via `GetPropW("PivoxDragOwner")` + `EnumChildWindows` and posts `WM_USER+100`.

### Cursor feedback

Uses `SetSystemCursor` to globally replace the arrow cursor during drag. OLE drag cursors loaded from `ole32.dll` (resource IDs 1=no-drop, 2=move, 3=copy, 4=link). Cursor selection based on `DROPEFFECT` returned by `IDropTarget::DragOver`. Restored via `SystemParametersInfoW(SPI_SETCURSORS)` on cleanup. SEH (`__try/__except`) wraps all `IDropTarget` calls to ensure cursor restore on crash.

## Safety

- **Cross-process protection:** `GetWindowThreadProcessId` check prevents calling `IDropTarget` pointers from other processes (which would crash — the pointer is only valid in the owning process's address space).
- **SEH on IDropTarget calls:** All `DragEnter/DragOver/DragLeave/Drop` calls are wrapped in `__try/__except` to protect against corrupt vtable pointers from `GetProp`.
- **Cursor restore:** `SystemParametersInfoW(SPI_SETCURSORS)` in `Cleanup()` always runs, including on cancel and SEH catch paths. `OnDestroy` cancels any active drag.

## DragService Abstraction

The shared WinRT component (`PivoxShared`) should expose a `DragService::StartDrag(DataPackage)` that the XAML UI calls. The implementation is selected at compile time via `PIVOX_ACTIVEX_HOST`:

```cpp
void DragService::StartDrag(DataPackage const& data) {
#ifdef PIVOX_ACTIVEX_HOST
    // Build IDataObject from DataPackage, post to ATL HWND for manual drag
#else
    // Standard WinUI: UIElement.StartDragAsync(data)
#endif
}
```

Same pattern already used for desktop Win32 API access in the shared component pch.h.

## Upstream fix

If [microsoft/microsoft-ui-xaml#7690](https://github.com/microsoft/microsoft-ui-xaml/issues/7690) is resolved and WinUI native drag works elevated, the `PIVOX_ACTIVEX_HOST` path can switch to native WinUI drag and the manual system can be removed.
