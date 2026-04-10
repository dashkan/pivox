#pragma once
// DragSource — manual in-process drag-drop for XAML Islands.
//
// WinUI's compositor steals all mouse messages from the Win32 message queue,
// starving DoDragDrop's modal loop. WinUI's native drag (IDragOperation)
// fails elevated (microsoft/microsoft-ui-xaml#7690).
//
// This implements drag via timer polling + direct IDropTarget calls:
// 1. DragStarting → build IDataObject → start 16ms timer
// 2. WM_TIMER → GetCursorPos + GetAsyncKeyState → WindowFromPoint →
//    GetProp("OleDropTargetInterface") → IDropTarget::DragEnter/Over/Drop
// 3. Same-process only (cross-process IDropTarget pointers are invalid)
// 4. Cursor via SetSystemCursor (compositor overrides normal SetCursor)
// 5. SEH wraps all IDropTarget calls (corrupt vtable protection)

#ifndef OCR_NORMAL
#define OCR_NORMAL 32512
#endif

namespace pivox {

class DragSource {
public:
    static constexpr UINT_PTR TIMER_ID = 42;
    static constexpr UINT TIMER_MS = 16;

    // Start a manual drag with the given IDataObject.
    // The timer fires on ownerHwnd's message loop.
    void Start(HWND ownerHwnd, IDataObject* dataObj);

    // Called from WM_TIMER handler.
    void Tick();

    // Is a drag in progress?
    bool IsActive() const { return active_; }

    // Which HWND owns the timer?
    HWND OwnerHwnd() const { return ownerHwnd_; }

    // Cancel any active drag (called from OnDestroy).
    void Cancel();

private:
    void Drop();
    void Cleanup();

    // SEH-protected IDropTarget wrappers.
    static HRESULT SafeDragEnter(IDropTarget* t, IDataObject* d, DWORD keys, POINTL pt, DWORD* eff);
    static HRESULT SafeDragOver(IDropTarget* t, DWORD keys, POINTL pt, DWORD* eff);
    static HRESULT SafeDragLeave(IDropTarget* t);
    static HRESULT SafeDrop(IDropTarget* t, IDataObject* d, DWORD keys, POINTL pt, DWORD* eff);

    HCURSOR CursorForEffect(DWORD effect);

    IDataObject* dataObj_ = nullptr;
    IDropTarget* currentTarget_ = nullptr;
    HWND currentTargetHwnd_ = nullptr;
    DWORD lastEffect_ = 0;
    bool active_ = false;
    HWND ownerHwnd_ = nullptr;

    // OLE drag cursors from ole32.dll (resource IDs 1-4).
    HCURSOR curNone_ = nullptr;
    HCURSOR curMove_ = nullptr;
    HCURSOR curCopy_ = nullptr;
    HCURSOR curLink_ = nullptr;
    HCURSOR lastCursorSet_ = nullptr;
};

// Process-wide singleton — only one drag at a time.
DragSource& GetDragSource();

} // namespace pivox
