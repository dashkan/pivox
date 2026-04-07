// PivoxActiveX.cpp : Implementation of DLL Exports.

#include "pch.h"
#include "framework.h"
#include "resource.h"
#include "PivoxActiveX_i.h"
#include "dllmain.h"

using namespace ATL;

_Use_decl_annotations_
STDAPI DllCanUnloadNow(void)
{
    return _AtlModule.DllCanUnloadNow();
}

_Use_decl_annotations_
STDAPI DllGetClassObject(_In_ REFCLSID rclsid, _In_ REFIID riid, _Outptr_ LPVOID* ppv)
{
    return _AtlModule.DllGetClassObject(rclsid, riid, ppv);
}

_Use_decl_annotations_
STDAPI DllRegisterServer(void)
{
    return _AtlModule.DllRegisterServer();
}

_Use_decl_annotations_
STDAPI DllUnregisterServer(void)
{
    return _AtlModule.DllUnregisterServer();
}

STDAPI DllInstall(BOOL bInstall, _In_opt_ LPCWSTR pszCmdLine)
{
    static const wchar_t szUserSwitch[] = L"user";

    if (pszCmdLine != nullptr)
    {
        if (_wcsnicmp(pszCmdLine, szUserSwitch, _countof(szUserSwitch)) == 0)
        {
            AtlSetPerUserRegistration(true);
        }
    }

    HRESULT hr = bInstall ? DllRegisterServer() : DllUnregisterServer();
    if (bInstall && FAILED(hr))
    {
        DllUnregisterServer();
    }
    return hr;
}
