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

## Popup Window Drag (Solved)

Popup windows use the same `DragService::HandleDragStarting()` call. The `DragService` implementation on the ActiveX path locates the ATL HWND via `GetPropW("PivoxDragOwner")` + `EnumChildWindows` and posts `WM_USER+100` to start the manual drag timer. This works from any XAML content in the process — main panel or popup.

### Cursor feedback

Uses `SetSystemCursor` to globally replace the arrow cursor during drag. OLE drag cursors loaded from `ole32.dll` (resource IDs 1=no-drop, 2=move, 3=copy, 4=link). Cursor selection based on `DROPEFFECT` returned by `IDropTarget::DragOver`. Restored via `SystemParametersInfoW(SPI_SETCURSORS)` on cleanup. SEH (`__try/__except`) wraps all `IDropTarget` calls to ensure cursor restore on crash.

## Safety

- **Cross-process protection:** `GetWindowThreadProcessId` check prevents calling `IDropTarget` pointers from other processes (which would crash — the pointer is only valid in the owning process's address space).
- **SEH on IDropTarget calls:** All `DragEnter/DragOver/DragLeave/Drop` calls are wrapped in `__try/__except` to protect against corrupt vtable pointers from `GetProp`.
- **Cursor restore:** `SystemParametersInfoW(SPI_SETCURSORS)` in `Cleanup()` always runs, including on cancel and SEH catch paths. `OnDestroy` cancels any active drag.

## DragService Abstraction (Solved)

`DragService` in `shared/DragService.h/.cpp` provides a compile-time abstraction. The XAML UI calls `DragService::HandleDragStarting(args, text)` identically in both targets. The `PIVOX_ACTIVEX_HOST` preprocessor selects the implementation:

- **ActiveX path** — Cancels the WinUI drag, builds `IDataObject` from the text payload, and posts to the ATL HWND to start the manual in-process drag timer.
- **App path** — No-op. The caller already set data on `args.Data()`, and native WinUI handles everything.

```cpp
// Usage in any XAML page (shared source):
element.DragStarting([](auto&&, DragStartingEventArgs const& args) {
    args.Data().SetText(L"my payload");
    args.AllowedOperations(DataPackageOperation::Copy);
    Pivox::DragService::HandleDragStarting(args, L"my payload");
});
```

The text is passed explicitly to `HandleDragStarting` to avoid async extraction from `DataPackage`, which fails with an STA assertion in ActiveX hosts.

## iNEWS Implemented Categories

The ActiveX control registers the following COM categories in its RGS file:

| Category | GUID | Purpose |
|----------|------|---------|
| MOS Item Browser | `{F5D13911-8FD9-11D4-9512-00C04F1E7663}` | Required for iNEWS to discover and load the plugin as a MOS item browser |

## Upstream fix

If [microsoft/microsoft-ui-xaml#7690](https://github.com/microsoft/microsoft-ui-xaml/issues/7690) is resolved and WinUI native drag works elevated, the `PIVOX_ACTIVEX_HOST` path can switch to native WinUI drag and the manual system can be removed.
