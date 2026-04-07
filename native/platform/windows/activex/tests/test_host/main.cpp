#include <windows.h>
#include <atlbase.h>
#include <atlcom.h>
#include <olectl.h>

#include "PivoxActiveX_i.h"

// Minimal Win32 test container for the PivoxActiveX control.
// Used for manual testing and automated UI Automation (UIA) tests.

static const wchar_t* kWindowClass = L"PivoxTestHost";
static const wchar_t* kWindowTitle = L"Pivox ActiveX Test Host";

static ATL::CComPtr<IOleObject> g_oleObject;
static ATL::CComPtr<IPivoxControl> g_pivoxControl;

// Minimal IOleClientSite for hosting the ActiveX control.
class TestClientSite : public IOleClientSite, public IOleInPlaceSite {
    LONG refCount_ = 1;
    HWND hwnd_ = nullptr;

public:
    explicit TestClientSite(HWND hwnd) : hwnd_(hwnd) {}

    // IUnknown
    STDMETHOD(QueryInterface)(REFIID riid, void** ppv) override {
        if (riid == IID_IUnknown || riid == IID_IOleClientSite) {
            *ppv = static_cast<IOleClientSite*>(this);
        } else if (riid == IID_IOleInPlaceSite) {
            *ppv = static_cast<IOleInPlaceSite*>(this);
        } else {
            *ppv = nullptr;
            return E_NOINTERFACE;
        }
        AddRef();
        return S_OK;
    }
    STDMETHOD_(ULONG, AddRef)() override { return InterlockedIncrement(&refCount_); }
    STDMETHOD_(ULONG, Release)() override {
        ULONG ref = InterlockedDecrement(&refCount_);
        if (ref == 0) delete this;
        return ref;
    }

    // IOleClientSite
    STDMETHOD(SaveObject)() override { return E_NOTIMPL; }
    STDMETHOD(GetMoniker)(DWORD, DWORD, IMoniker**) override { return E_NOTIMPL; }
    STDMETHOD(GetContainer)(IOleContainer**) override { return E_NOTIMPL; }
    STDMETHOD(ShowObject)() override { return S_OK; }
    STDMETHOD(OnShowWindow)(BOOL) override { return S_OK; }
    STDMETHOD(RequestNewObjectLayout)() override { return E_NOTIMPL; }

    // IOleWindow (base of IOleInPlaceSite)
    STDMETHOD(GetWindow)(HWND* phwnd) override { *phwnd = hwnd_; return S_OK; }
    STDMETHOD(ContextSensitiveHelp)(BOOL) override { return E_NOTIMPL; }

    // IOleInPlaceSite
    STDMETHOD(CanInPlaceActivate)() override { return S_OK; }
    STDMETHOD(OnInPlaceActivate)() override { return S_OK; }
    STDMETHOD(OnUIActivate)() override { return S_OK; }
    STDMETHOD(GetWindowContext)(IOleInPlaceFrame** ppFrame, IOleInPlaceUIWindow** ppDoc,
                                LPRECT lprcPosRect, LPRECT lprcClipRect,
                                LPOLEINPLACEFRAMEINFO lpFrameInfo) override {
        *ppFrame = nullptr;
        *ppDoc = nullptr;
        GetClientRect(hwnd_, lprcPosRect);
        *lprcClipRect = *lprcPosRect;
        lpFrameInfo->fMDIApp = FALSE;
        lpFrameInfo->hwndFrame = hwnd_;
        lpFrameInfo->haccel = nullptr;
        lpFrameInfo->cAccelEntries = 0;
        return S_OK;
    }
    STDMETHOD(Scroll)(SIZE) override { return E_NOTIMPL; }
    STDMETHOD(OnUIDeactivate)(BOOL) override { return S_OK; }
    STDMETHOD(OnInPlaceDeactivate)() override { return S_OK; }
    STDMETHOD(DiscardUndoState)() override { return E_NOTIMPL; }
    STDMETHOD(DeactivateAndUndo)() override { return E_NOTIMPL; }
    STDMETHOD(OnPosRectChange)(LPCRECT) override { return S_OK; }
};

static LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wParam, LPARAM lParam) {
    switch (msg) {
    case WM_SIZE:
        if (g_oleObject) {
            RECT rc;
            GetClientRect(hwnd, &rc);
            ATL::CComPtr<IOleInPlaceObject> inPlace;
            if (SUCCEEDED(g_oleObject.QueryInterface(&inPlace))) {
                inPlace->SetObjectRects(&rc, &rc);
            }
        }
        return 0;
    case WM_DESTROY:
        if (g_pivoxControl) {
            g_pivoxControl->Shutdown();
            g_pivoxControl.Release();
        }
        if (g_oleObject) {
            g_oleObject->Close(OLECLOSE_NOSAVE);
            g_oleObject.Release();
        }
        PostQuitMessage(0);
        return 0;
    }
    return DefWindowProcW(hwnd, msg, wParam, lParam);
}

int WINAPI wWinMain(HINSTANCE hInstance, HINSTANCE, LPWSTR, int nCmdShow) {
    CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);

    WNDCLASSEXW wc = {};
    wc.cbSize = sizeof(wc);
    wc.lpfnWndProc = WndProc;
    wc.hInstance = hInstance;
    wc.hCursor = LoadCursorW(nullptr, IDC_ARROW);
    wc.hbrBackground = reinterpret_cast<HBRUSH>(COLOR_WINDOW + 1);
    wc.lpszClassName = kWindowClass;
    RegisterClassExW(&wc);

    HWND hwnd = CreateWindowExW(0, kWindowClass, kWindowTitle,
                                WS_OVERLAPPEDWINDOW,
                                CW_USEDEFAULT, CW_USEDEFAULT, 800, 600,
                                nullptr, nullptr, hInstance, nullptr);

    // Create and activate the ActiveX control.
    HRESULT hr = g_oleObject.CoCreateInstance(CLSID_PivoxControl);
    if (SUCCEEDED(hr)) {
        auto clientSite = new TestClientSite(hwnd);
        g_oleObject->SetClientSite(clientSite);
        clientSite->Release();

        RECT rc;
        GetClientRect(hwnd, &rc);
        g_oleObject->DoVerb(OLEIVERB_INPLACEACTIVATE, nullptr,
                            nullptr, 0, hwnd, &rc);
        g_oleObject.QueryInterface(&g_pivoxControl);
    }

    ShowWindow(hwnd, nCmdShow);
    UpdateWindow(hwnd);

    MSG msg;
    while (GetMessageW(&msg, nullptr, 0, 0)) {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }

    CoUninitialize();
    return static_cast<int>(msg.wParam);
}
