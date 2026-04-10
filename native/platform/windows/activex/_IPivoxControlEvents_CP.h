#pragma once

// {E5F6A7B8-C9D0-4E1F-8A2B-3C4D5E6F7A8B}
struct __declspec(uuid("E5F6A7B8-C9D0-4E1F-8A2B-3C4D5E6F7A8B")) _IPivoxControlEvents;

using namespace ATL;

template <class T>
class CProxy_IPivoxControlEvents :
    public IConnectionPointImpl<T, &__uuidof(_IPivoxControlEvents), CComDynamicUnkArray>
{
public:
    HRESULT Fire_OnButtonClicked(BSTR buttonName)
    {
        HRESULT hr = S_OK;
        T* pThis = static_cast<T*>(this);
        int cConnections = this->m_vec.GetSize();
        for (int iConnection = 0; iConnection < cConnections; iConnection++) {
            pThis->Lock();
            CComPtr<IUnknown> punkConnection = this->m_vec.GetAt(iConnection);
            pThis->Unlock();
            IDispatch* pConnection = static_cast<IDispatch*>(punkConnection.p);
            if (pConnection) {
                CComVariant varResult;
                VARIANT params[1];
                params[0].vt = VT_BSTR;
                params[0].bstrVal = buttonName;
                DISPPARAMS dp = { params, nullptr, 1, 0 };
                hr = pConnection->Invoke(1, IID_NULL, LOCALE_USER_DEFAULT,
                    DISPATCH_METHOD, &dp, &varResult, nullptr, nullptr);
            }
        }
        return hr;
    }

    HRESULT Fire_mosMsgFromPlugIn(BSTR mosMsg)
    {
        HRESULT hr = S_OK;
        T* pThis = static_cast<T*>(this);
        int cConnections = this->m_vec.GetSize();
        for (int iConnection = 0; iConnection < cConnections; iConnection++) {
            pThis->Lock();
            CComPtr<IUnknown> punkConnection = this->m_vec.GetAt(iConnection);
            pThis->Unlock();
            IDispatch* pConnection = static_cast<IDispatch*>(punkConnection.p);
            if (pConnection) {
                CComVariant varResult;
                VARIANT params[1];
                params[0].vt = VT_BSTR;
                params[0].bstrVal = mosMsg;
                DISPPARAMS dp = { params, nullptr, 1, 0 };
                hr = pConnection->Invoke(201, IID_NULL, LOCALE_USER_DEFAULT,
                    DISPATCH_METHOD, &dp, &varResult, nullptr, nullptr);
            }
        }
        return hr;
    }
};
