#include "pch.h"
#include "DragSource.h"

namespace pivox {

static DragSource s_dragSource;

DragSource& GetDragSource() { return s_dragSource; }

// ============================================================
// SEH wrappers — protect against corrupt IDropTarget vtable
// pointers from GetProp("OleDropTargetInterface").
// ============================================================
HRESULT DragSource::SafeDragEnter(IDropTarget* t, IDataObject* d, DWORD keys, POINTL pt, DWORD* eff) {
    __try { return t->DragEnter(d, keys, pt, eff); }
    __except (EXCEPTION_EXECUTE_HANDLER) { return E_FAIL; }
}
HRESULT DragSource::SafeDragOver(IDropTarget* t, DWORD keys, POINTL pt, DWORD* eff) {
    __try { return t->DragOver(keys, pt, eff); }
    __except (EXCEPTION_EXECUTE_HANDLER) { return E_FAIL; }
}
HRESULT DragSource::SafeDragLeave(IDropTarget* t) {
    __try { return t->DragLeave(); }
    __except (EXCEPTION_EXECUTE_HANDLER) { return E_FAIL; }
}
HRESULT DragSource::SafeDrop(IDropTarget* t, IDataObject* d, DWORD keys, POINTL pt, DWORD* eff) {
    __try { return t->Drop(d, keys, pt, eff); }
    __except (EXCEPTION_EXECUTE_HANDLER) { return E_FAIL; }
}

HCURSOR DragSource::CursorForEffect(DWORD effect) {
    if (effect & DROPEFFECT_LINK) return curLink_;
    if (effect & DROPEFFECT_COPY) return curCopy_;
    if (effect & DROPEFFECT_MOVE) return curMove_;
    return curNone_;
}

void DragSource::Start(HWND ownerHwnd, IDataObject* dataObj) {
    if (active_) return;

    dataObj_ = dataObj;
    dataObj_->AddRef();
    active_ = true;
    ownerHwnd_ = ownerHwnd;
    currentTarget_ = nullptr;
    currentTargetHwnd_ = nullptr;
    lastEffect_ = 0;
    lastCursorSet_ = nullptr;

    // OLE drag cursors from ole32.dll (resource IDs 1-4).
    HMODULE hOle32 = ::GetModuleHandleW(L"ole32.dll");
    curNone_ = ::LoadCursorW(hOle32, MAKEINTRESOURCEW(1));
    curMove_ = ::LoadCursorW(hOle32, MAKEINTRESOURCEW(2));
    curCopy_ = ::LoadCursorW(hOle32, MAKEINTRESOURCEW(3));
    curLink_ = ::LoadCursorW(hOle32, MAKEINTRESOURCEW(4));
    // Fallbacks if ole32 cursors aren't available.
    if (!curNone_) curNone_ = ::LoadCursorW(nullptr, MAKEINTRESOURCEW(32648)); // IDC_NO
    if (!curMove_) curMove_ = ::LoadCursorW(nullptr, MAKEINTRESOURCEW(32512)); // IDC_ARROW
    if (!curCopy_) curCopy_ = ::LoadCursorW(nullptr, MAKEINTRESOURCEW(32512)); // IDC_ARROW
    if (!curLink_) curLink_ = ::LoadCursorW(nullptr, MAKEINTRESOURCEW(32649)); // IDC_HAND

    ::SetSystemCursor((HCURSOR)::CopyIcon((HICON)curNone_), OCR_NORMAL);
    ::SetTimer(ownerHwnd, TIMER_ID, TIMER_MS, nullptr);
    OutputDebugStringW(L"[PivoxActiveX] DragSource: started\n");
}

void DragSource::Tick() {
    if (!active_) return;

    if (!(::GetAsyncKeyState(VK_LBUTTON) & 0x8000)) {
        Drop();
        return;
    }
    if (::GetAsyncKeyState(VK_ESCAPE) & 0x8000) {
        Cancel();
        return;
    }

    POINT pt;
    ::GetCursorPos(&pt);
    HWND hwndUnder = ::WindowFromPoint(pt);

    // Walk up to find a window with a registered IDropTarget (same process only).
    // GetProp("OleDropTargetInterface") returns a raw COM pointer —
    // only valid in the owning process's address space.
    DWORD ourPid = ::GetCurrentProcessId();
    IDropTarget* target = nullptr;
    HWND targetHwnd = nullptr;
    for (HWND h = hwndUnder; h; h = ::GetParent(h)) {
        DWORD pid = 0;
        ::GetWindowThreadProcessId(h, &pid);
        if (pid != ourPid) break;
        auto* dt = static_cast<IDropTarget*>(::GetPropW(h, L"OleDropTargetInterface"));
        if (dt) {
            target = dt;
            targetHwnd = h;
            break;
        }
    }

    if (targetHwnd != currentTargetHwnd_) {
        if (currentTarget_) {
            SafeDragLeave(currentTarget_);
        }
        currentTarget_ = target;
        currentTargetHwnd_ = targetHwnd;
        if (currentTarget_) {
            DWORD effect = DROPEFFECT_COPY | DROPEFFECT_MOVE | DROPEFFECT_LINK;
            HRESULT hr = SafeDragEnter(currentTarget_, dataObj_, MK_LBUTTON, { pt.x, pt.y }, &effect);
            if (FAILED(hr)) {
                OutputDebugStringW(L"[PivoxActiveX] DragSource: DragEnter failed (SEH)\n");
                currentTarget_ = nullptr;
                currentTargetHwnd_ = nullptr;
            } else {
                lastEffect_ = effect;
            }
        }
    } else if (currentTarget_) {
        DWORD effect = DROPEFFECT_COPY | DROPEFFECT_MOVE | DROPEFFECT_LINK;
        if (SUCCEEDED(SafeDragOver(currentTarget_, MK_LBUTTON, { pt.x, pt.y }, &effect)))
            lastEffect_ = effect;
    }

    // Update system cursor only when it changes.
    HCURSOR desired = currentTarget_ ? CursorForEffect(lastEffect_) : curNone_;
    if (desired != lastCursorSet_) {
        ::SetSystemCursor((HCURSOR)::CopyIcon((HICON)desired), OCR_NORMAL);
        lastCursorSet_ = desired;
    }
}

void DragSource::Drop() {
    if (currentTarget_ && (lastEffect_ & (DROPEFFECT_COPY | DROPEFFECT_MOVE | DROPEFFECT_LINK))) {
        POINT pt;
        ::GetCursorPos(&pt);
        DWORD effect = DROPEFFECT_COPY | DROPEFFECT_MOVE | DROPEFFECT_LINK;
        HRESULT hr = SafeDrop(currentTarget_, dataObj_, MK_LBUTTON, { pt.x, pt.y }, &effect);
        wchar_t buf[128];
        swprintf_s(buf, _countof(buf), L"[PivoxActiveX] DragSource: Drop hr=0x%08X effect=0x%X\n", hr, effect);
        OutputDebugStringW(buf);
    } else {
        if (currentTarget_) SafeDragLeave(currentTarget_);
        OutputDebugStringW(L"[PivoxActiveX] DragSource: dropped on non-target\n");
    }
    Cleanup();
}

void DragSource::Cancel() {
    if (currentTarget_) SafeDragLeave(currentTarget_);
    OutputDebugStringW(L"[PivoxActiveX] DragSource: cancelled\n");
    Cleanup();
}

void DragSource::Cleanup() {
    active_ = false;
    currentTarget_ = nullptr;
    currentTargetHwnd_ = nullptr;
    lastCursorSet_ = nullptr;
    ::KillTimer(ownerHwnd_, TIMER_ID);
    // Always restore system cursors.
    ::SystemParametersInfoW(SPI_SETCURSORS, 0, nullptr, 0);
    if (dataObj_) { dataObj_->Release(); dataObj_ = nullptr; }
    curNone_ = curMove_ = curCopy_ = curLink_ = nullptr;
}

} // namespace pivox
