#pragma once
#include "resource.h"
#include <atlctl.h>
#include "PivoxActiveX_i.h"
#include "_IPivoxControlEvents_CP.h"
#include "XamlIslandHost.h"

using namespace ATL;

class ATL_NO_VTABLE CPivoxControl :
    public CComObjectRootEx<CComSingleThreadModel>,
    public IDispatchImpl<IPivoxControl, &IID_IPivoxControl, &LIBID_PivoxActiveXLib, 1, 0>,
    public IOleControlImpl<CPivoxControl>,
    public IOleObjectImpl<CPivoxControl>,
    public IOleInPlaceActiveObjectImpl<CPivoxControl>,
    public IViewObjectExImpl<CPivoxControl>,
    public IOleInPlaceObjectWindowlessImpl<CPivoxControl>,
    public IConnectionPointContainerImpl<CPivoxControl>,
    public CProxy_IPivoxControlEvents<CPivoxControl>,
    public IPersistStreamInitImpl<CPivoxControl>,
    public IProvideClassInfo2Impl<&CLSID_PivoxControl, &__uuidof(_IPivoxControlEvents), &LIBID_PivoxActiveXLib>,
    public CComCoClass<CPivoxControl, &CLSID_PivoxControl>,
    public CComControl<CPivoxControl>
{
public:
    CPivoxControl();

DECLARE_OLEMISC_STATUS(OLEMISC_RECOMPOSEONRESIZE |
    OLEMISC_CANTLINKINSIDE |
    OLEMISC_INSIDEOUT |
    OLEMISC_ACTIVATEWHENVISIBLE |
    OLEMISC_SETCLIENTSITEFIRST
)

DECLARE_REGISTRY_RESOURCEID(IDR_PIVOXCONTROL)

BEGIN_COM_MAP(CPivoxControl)
    COM_INTERFACE_ENTRY(IPivoxControl)
    COM_INTERFACE_ENTRY(IDispatch)
    COM_INTERFACE_ENTRY(IViewObjectEx)
    COM_INTERFACE_ENTRY(IViewObject2)
    COM_INTERFACE_ENTRY(IViewObject)
    COM_INTERFACE_ENTRY(IOleInPlaceObjectWindowless)
    COM_INTERFACE_ENTRY(IOleInPlaceObject)
    COM_INTERFACE_ENTRY2(IOleWindow, IOleInPlaceObjectWindowlessImpl)
    COM_INTERFACE_ENTRY(IOleInPlaceActiveObject)
    COM_INTERFACE_ENTRY(IOleControl)
    COM_INTERFACE_ENTRY(IOleObject)
    COM_INTERFACE_ENTRY(IPersistStreamInit)
    COM_INTERFACE_ENTRY2(IPersist, IPersistStreamInit)
    COM_INTERFACE_ENTRY(IConnectionPointContainer)
    COM_INTERFACE_ENTRY(IProvideClassInfo)
    COM_INTERFACE_ENTRY(IProvideClassInfo2)
END_COM_MAP()

BEGIN_PROP_MAP(CPivoxControl)
    PROP_DATA_ENTRY("_cx", m_sizeExtent.cx, VT_UI4)
    PROP_DATA_ENTRY("_cy", m_sizeExtent.cy, VT_UI4)
END_PROP_MAP()

BEGIN_CONNECTION_POINT_MAP(CPivoxControl)
    CONNECTION_POINT_ENTRY(__uuidof(_IPivoxControlEvents))
END_CONNECTION_POINT_MAP()

BEGIN_MSG_MAP(CPivoxControl)
    CHAIN_MSG_MAP(CComControl<CPivoxControl>)
    MESSAGE_HANDLER(WM_CREATE, OnCreate)
    MESSAGE_HANDLER(WM_DESTROY, OnDestroy)
    MESSAGE_HANDLER(WM_SIZE, OnSize)
    MESSAGE_HANDLER(WM_MOVE, OnMove)
    MESSAGE_HANDLER(WM_TIMER, OnTimer)
    MESSAGE_HANDLER(WM_USER + 100, OnStartManualDrag)
    DEFAULT_REFLECTION_HANDLER()
END_MSG_MAP()

    DECLARE_VIEW_STATUS(VIEWSTATUS_SOLIDBKGND | VIEWSTATUS_OPAQUE)

    HRESULT OnDraw(ATL_DRAWINFO& di);
    STDMETHOD(InPlaceDeactivate)() override;

    // IPivoxControl
    STDMETHOD(mosMsgFromHost)(BSTR mosMsg, BSTR* mosResponse) override;

    DECLARE_PROTECT_FINAL_CONSTRUCT()
    HRESULT FinalConstruct() { return S_OK; }
    void FinalRelease();

private:
    LRESULT OnCreate(UINT, WPARAM, LPARAM, BOOL& bHandled);
    LRESULT OnDestroy(UINT, WPARAM, LPARAM, BOOL& bHandled);
    LRESULT OnSize(UINT, WPARAM, LPARAM, BOOL& bHandled);
    LRESULT OnMove(UINT, WPARAM, LPARAM, BOOL& bHandled);
    LRESULT OnTimer(UINT, WPARAM, LPARAM, BOOL& bHandled);
    LRESULT OnStartManualDrag(UINT, WPARAM, LPARAM, BOOL& bHandled);

    pivox::XamlIslandHost host_;
    pivox::IslandSlot* islandSlot_ = nullptr;
    std::shared_ptr<bool> aliveFlag_;
    bool xamlInitialized_ = false;
};

OBJECT_ENTRY_AUTO(__uuidof(PivoxControl), CPivoxControl)
