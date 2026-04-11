#include "pch.h"
#include "PivoxControl.h"
#include "XamlIslandHost.h"
#include "DragSource.h"
#include "DragService.h"
#include "PivoxServices.h"
#include "WinAppState.h"
#include "WinAuthService.h"
#include "firebase_config.h"

#include <string>
#include <shlobj.h>
#include <winrt/Pivox.h>

CPivoxControl::CPivoxControl() {
    m_bWindowOnly = TRUE;
}

void CPivoxControl::FinalRelease() {
    // Slot cleanup handled by OnDestroy.
}

HRESULT CPivoxControl::OnDraw(ATL_DRAWINFO& di) {
    if (islandSlot_) return S_OK;

    RECT& rc = *const_cast<RECT*>(reinterpret_cast<const RECT*>(di.prcBounds));
    Rectangle(di.hdcDraw, rc.left, rc.top, rc.right, rc.bottom);
    SetTextAlign(di.hdcDraw, TA_CENTER | TA_BASELINE);
    LPCTSTR pszText = _T("PivoxControl");
    TextOut(di.hdcDraw,
        (rc.left + rc.right) / 2,
        (rc.top + rc.bottom) / 2,
        pszText, lstrlen(pszText));
    return S_OK;
}

LRESULT CPivoxControl::OnCreate(UINT, WPARAM, LPARAM, BOOL& bHandled) {
    bHandled = FALSE;

    try {
        // Initialize shared services (once per process).
        static bool s_servicesInit = false;
        if (!s_servicesInit) {
            auto appState = std::make_shared<pivox::WinAppState>();
            auto authService = std::make_shared<pivox::WinAuthService>();
            authService->initializeFirebase();
            authService->connectToEmulatorIfRequested();

            pivox::OAuthConfig oauthConfig;
            oauthConfig.googleClientId = pivox::firebase_config::kGoogleSignInClientId;
            authService->setOAuthConfig(oauthConfig);

            pivox::PivoxServices::initialize(appState, authService);
            s_servicesInit = true;
        }

        // Process-wide init (bootstrap, dispatcher, Application, theme).
        HRESULT hr = pivox::XamlIslandHost::InitializeProcess();
        if (FAILED(hr)) return 0;

        // Acquire a DWXS slot.
        {
            pivox::ScopedActCtx ctx;
            islandSlot_ = host_.AcquireSlot(m_hWnd);
        }
        if (!islandSlot_) {
            OutputDebugStringW(L"[PivoxActiveX] AcquireSlot returned null\n");
            return 0;
        }

        bool isReuse = (islandSlot_->source.Content() != nullptr);
        if (isReuse) {
            xamlInitialized_ = true;
            OutputDebugStringW(L"[PivoxActiveX] Slot reused\n");
            return 0;
        }

        // Load XamlControlsResources BEFORE creating any pages.
        // NavigationView, PersonPicture etc. need these resources during XAML parse.
        // Must be after DWXS.Initialize() (AcquireSlot above).
        host_.EnsureResources();

        // Auth state listener — swap content on sign-in/sign-out.
        // Firebase fires on a background thread, so dispatch to UI thread.
        auto dispatcher = winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread();
        pivox::PivoxServices::authService()->onAuthStateChanged(
            [this, dispatcher](bool signedIn) {
                dispatcher.TryEnqueue([this, signedIn]() {
                    if (!islandSlot_) return;
                    pivox::ScopedActCtx ctx;
                    auto factory = winrt::get_activation_factory<winrt::Windows::Foundation::IActivationFactory>(
                        winrt::hstring(signedIn ? L"Pivox.MainPage" : L"Pivox.LoginPage"));
                    auto page = factory.ActivateInstance<winrt::Microsoft::UI::Xaml::UIElement>();
                    islandSlot_->source.Content(page);
                    OutputDebugStringW(signedIn
                        ? L"[PivoxActiveX] Switched to MainPage\n"
                        : L"[PivoxActiveX] Switched to LoginPage\n");
                });
            });

        // Initial content based on current auth state.
        {
            pivox::ScopedActCtx ctx;
            bool signedIn = pivox::PivoxServices::authService()->isSignedIn();
            auto factory = winrt::get_activation_factory<winrt::Windows::Foundation::IActivationFactory>(
                winrt::hstring(signedIn ? L"Pivox.MainPage" : L"Pivox.LoginPage"));
            auto page = factory.ActivateInstance<winrt::Microsoft::UI::Xaml::UIElement>();
            islandSlot_->source.Content(page);
        }

        // Expose HWND for popup window drag bridge.
        ::SetPropW(m_hWnd, L"PivoxDragOwner", (HANDLE)1);

        xamlInitialized_ = true;
        OutputDebugStringW(L"[PivoxActiveX] New slot ready\n");

    } catch (const winrt::hresult_error& ex) {
        xamlInitialized_ = false;
        wchar_t buf[1024];
        swprintf_s(buf, _countof(buf), L"[PivoxActiveX] XAML error: 0x%08X %s\n",
            static_cast<int32_t>(ex.code()), ex.message().c_str());
        OutputDebugStringW(buf);
    }

    return 0;
}

LRESULT CPivoxControl::OnDestroy(UINT, WPARAM, LPARAM, BOOL& bHandled) {
    bHandled = FALSE;
    auto& drag = pivox::GetDragSource();
    if (drag.IsActive() && drag.OwnerHwnd() == m_hWnd) {
        drag.Cancel();
    }
    ::RemovePropW(m_hWnd, L"PivoxDragOwner");
    xamlInitialized_ = false;
    if (islandSlot_) {
        host_.ReleaseSlot(islandSlot_);
        islandSlot_ = nullptr;
        OutputDebugStringW(L"[PivoxActiveX] Slot released\n");
    }
    return 0;
}

LRESULT CPivoxControl::OnSize(UINT, WPARAM, LPARAM, BOOL& bHandled) {
    if (xamlInitialized_ && islandSlot_)
        host_.UpdatePosition(islandSlot_, m_hWnd);
    bHandled = TRUE;
    return 0;
}

LRESULT CPivoxControl::OnMove(UINT, WPARAM, LPARAM, BOOL& bHandled) {
    if (xamlInitialized_ && islandSlot_)
        host_.UpdatePosition(islandSlot_, m_hWnd);
    bHandled = FALSE;
    return 0;
}

LRESULT CPivoxControl::OnTimer(UINT, WPARAM wParam, LPARAM, BOOL& bHandled) {
    if (wParam == pivox::DragSource::TIMER_ID) {
        pivox::GetDragSource().Tick();
        bHandled = TRUE;
    } else {
        bHandled = FALSE;
    }
    return 0;
}

LRESULT CPivoxControl::OnStartManualDrag(UINT, WPARAM, LPARAM, BOOL& bHandled) {
    bHandled = TRUE;
    auto& drag = pivox::GetDragSource();
    if (drag.IsActive()) return 0;

    // Pick up the IDataObject stored by DragService.
    auto* pDataObj = static_cast<IDataObject*>(::GetPropW(m_hWnd, L"PivoxDragData"));
    ::RemovePropW(m_hWnd, L"PivoxDragData");

    if (!pDataObj) {
        OutputDebugStringW(L"[PivoxActiveX] OnStartManualDrag: no drag data\n");
        return 0;
    }

    OutputDebugStringW(L"[PivoxActiveX] OnStartManualDrag: starting\n");
    drag.Start(m_hWnd, pDataObj);
    pDataObj->Release();  // DragSource::Start AddRef'd it
    return 0;
}

// MOS Protocol v2.8.5
STDMETHODIMP CPivoxControl::mosMsgFromHost(BSTR mosMsg, BSTR* mosResponse) {
    if (!mosResponse) return E_POINTER;

    UINT len = ::SysStringLen(mosMsg);
    wchar_t buf[256];
    swprintf_s(buf, _countof(buf), L"[PivoxActiveX] mosMsgFromHost: %u chars\n", len);
    OutputDebugStringW(buf);

    if (mosMsg && len > 0) {
        size_t previewLen = (len < 200) ? len : 200;
        std::wstring preview(mosMsg, previewLen);
        OutputDebugStringW((L"[PivoxActiveX] mosMsg: " + preview + L"\n").c_str());
    }

    *mosResponse = ::SysAllocString(L"<ncsAck><status>ACK</status></ncsAck>");
    return S_OK;
}

STDMETHODIMP CPivoxControl::InPlaceDeactivate() {
    auto& drag = pivox::GetDragSource();
    if (drag.IsActive() && drag.OwnerHwnd() == m_hWnd) {
        drag.Cancel();
    }
    xamlInitialized_ = false;
    if (islandSlot_) {
        host_.ReleaseSlot(islandSlot_);
        islandSlot_ = nullptr;
    }
    return IOleInPlaceObjectWindowlessImpl<CPivoxControl>::InPlaceDeactivate();
}
