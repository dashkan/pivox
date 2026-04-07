#include "pch.h"
#include "PivoxControl.h"
#include "PivoxServices.h"
#include "firebase_config.h"

#include <mutex>

// Process-wide init — Firebase C++ SDK must only be created once per process.
static std::once_flag s_servicesInit;
static HRESULT s_servicesInitResult = S_OK;

static void InitServicesOnce() {
    auto appState = std::make_shared<pivox::WinAppState>();
    auto authService = std::make_shared<pivox::WinAuthService>();

    if (!authService->initializeFirebase()) {
        s_servicesInitResult = E_FAIL;
        return;
    }

    authService->connectToEmulatorIfRequested();

    pivox::OAuthConfig oauthConfig;
    oauthConfig.googleClientId = pivox::firebase_config::kGoogleSignInClientId;
    authService->setOAuthConfig(oauthConfig);

    pivox::PivoxServices::initialize(appState, authService);
}

CPivoxControl::CPivoxControl() = default;

void CPivoxControl::FinalRelease() {
    dragSource_.Detach();
    xamlHost_.Shutdown();
}

HRESULT CPivoxControl::OnDraw(ATL_DRAWINFO& di) {
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

STDMETHODIMP CPivoxControl::DoVerb(
    LONG iVerb, LPMSG lpmsg, IOleClientSite* pActiveSite,
    LONG lindex, HWND hwndParent, LPCRECT lprcPosRect)
{
    if (iVerb == OLEIVERB_INPLACEACTIVATE || iVerb == OLEIVERB_UIACTIVATE ||
        iVerb == OLEIVERB_SHOW)
    {
        HRESULT hr = IOleObjectImpl<CPivoxControl>::DoVerb(
            iVerb, lpmsg, pActiveSite, lindex, hwndParent, lprcPosRect);
        if (FAILED(hr)) return hr;

        if (!xamlHost_.IsInitialized() && m_hWnd) {
            std::call_once(s_servicesInit, InitServicesOnce);
            if (FAILED(s_servicesInitResult)) return s_servicesInitResult;

            hr = xamlHost_.Initialize(m_hWnd);
            if (FAILED(hr)) return hr;

            xamlHost_.NavigateTo(L"Pivox.LoginPage");
        }
        return S_OK;
    }

    return IOleObjectImpl<CPivoxControl>::DoVerb(
        iVerb, lpmsg, pActiveSite, lindex, hwndParent, lprcPosRect);
}

STDMETHODIMP CPivoxControl::InPlaceDeactivate() {
    dragSource_.Detach();
    xamlHost_.Shutdown();
    return IOleInPlaceObjectWindowlessImpl<CPivoxControl>::InPlaceDeactivate();
}

STDMETHODIMP CPivoxControl::NavigateTo(BSTR pageName) {
    if (!pageName) return E_INVALIDARG;
    return xamlHost_.NavigateTo(pageName);
}

STDMETHODIMP CPivoxControl::Shutdown() {
    dragSource_.Detach();
    xamlHost_.Shutdown();
    return S_OK;
}

STDMETHODIMP CPivoxControl::get_IsInitialized(VARIANT_BOOL* pVal) {
    if (!pVal) return E_POINTER;
    *pVal = xamlHost_.IsInitialized() ? VARIANT_TRUE : VARIANT_FALSE;
    return S_OK;
}

LRESULT CPivoxControl::OnSize(UINT, WPARAM, LPARAM lParam, BOOL& bHandled) {
    xamlHost_.Resize(LOWORD(lParam), HIWORD(lParam));
    bHandled = TRUE;
    return 0;
}
