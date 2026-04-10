#include "pch.h"
#include "DragService.h"

#ifdef PIVOX_ACTIVEX_HOST
#include <shlobj.h>
#endif

namespace Pivox {

void DragService::HandleDragStarting(
    const winrt::Microsoft::UI::Xaml::DragStartingEventArgs& args,
    const winrt::hstring& text)
{
#ifdef PIVOX_ACTIVEX_HOST
    // Cancel WinUI's compositor drag — doesn't work elevated,
    // and its modal loop starves alongside XAML Islands.
    args.Cancel(true);

    // Build IDataObject with CF_UNICODETEXT.
    std::wstring payload(text.c_str(), text.size());
    size_t len = (payload.size() + 1) * sizeof(wchar_t);
    HGLOBAL hGlobal = ::GlobalAlloc(GMEM_MOVEABLE, len);
    if (!hGlobal) return;
    memcpy(::GlobalLock(hGlobal), payload.c_str(), len);
    ::GlobalUnlock(hGlobal);

    IDataObject* pDataObj = nullptr;
    ::SHCreateDataObject(nullptr, 0, nullptr, nullptr, IID_PPV_ARGS(&pDataObj));
    if (!pDataObj) { ::GlobalFree(hGlobal); return; }

    FORMATETC fmt = { CF_UNICODETEXT, nullptr, DVASPECT_CONTENT, -1, TYMED_HGLOBAL };
    STGMEDIUM stg = {};
    stg.tymed = TYMED_HGLOBAL;
    stg.hGlobal = hGlobal;
    pDataObj->SetData(&fmt, &stg, TRUE);

    // TODO: Register custom clipboard formats (MOS XML, etc.) from
    // DataPackage custom properties and add them to the IDataObject.

    // Find the ATL HWND via the PivoxDragOwner window property.
    HWND atlHwnd = nullptr;
    ::EnumChildWindows(::GetDesktopWindow(), [](HWND h, LPARAM lp) -> BOOL {
        if (::GetPropW(h, L"PivoxDragOwner")) {
            *reinterpret_cast<HWND*>(lp) = h;
            return FALSE;
        }
        return TRUE;
    }, reinterpret_cast<LPARAM>(&atlHwnd));

    if (atlHwnd) {
        // Pass IDataObject to ATL via window property.
        ::SetPropW(atlHwnd, L"PivoxDragData", static_cast<HANDLE>(pDataObj));
        ::PostMessageW(atlHwnd, WM_USER + 100, 0, 0);
        OutputDebugStringW(L"[DragService] ActiveX drag posted\n");
    } else {
        pDataObj->Release();
        OutputDebugStringW(L"[DragService] ATL HWND not found\n");
    }
#else
    // Native WinUI drag — caller already set data on args.Data().
    // Nothing to do — the compositor handles everything.
    (void)args;
    (void)text;
    OutputDebugStringW(L"[DragService] Native WinUI drag\n");
#endif
}

} // namespace Pivox
