#include <gtest/gtest.h>
#include <windows.h>
#include <atlbase.h>
#include <ocidl.h>
#include <olectl.h>

// MIDL-generated header for PivoxControl interfaces.
#include "PivoxActiveX_i.h"

class ComInterfaceTest : public ::testing::Test {
protected:
    void SetUp() override {
        HRESULT hr = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
        ASSERT_TRUE(SUCCEEDED(hr) || hr == S_FALSE);
        coInitialized_ = true;
    }

    void TearDown() override {
        control_.Release();
        if (coInitialized_) {
            CoUninitialize();
        }
    }

    HRESULT CreateControl() {
        return control_.CoCreateInstance(CLSID_PivoxControl);
    }

    ATL::CComPtr<IUnknown> control_;
    bool coInitialized_ = false;
};

TEST_F(ComInterfaceTest, CoCreateInstance_Succeeds) {
    HRESULT hr = CreateControl();
    ASSERT_HRESULT_SUCCEEDED(hr);
    ASSERT_NE(control_.p, nullptr);
}

TEST_F(ComInterfaceTest, QI_IPivoxControl) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IPivoxControl> pivox;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&pivox));
}

TEST_F(ComInterfaceTest, QI_IDispatch) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IDispatch> disp;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&disp));
}

TEST_F(ComInterfaceTest, QI_IOleObject) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IOleObject> oleObj;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&oleObj));
}

TEST_F(ComInterfaceTest, QI_IOleInPlaceObject) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IOleInPlaceObject> inPlace;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&inPlace));
}

TEST_F(ComInterfaceTest, QI_IOleInPlaceActiveObject) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IOleInPlaceActiveObject> inPlaceActive;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&inPlaceActive));
}

TEST_F(ComInterfaceTest, QI_IOleControl) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IOleControl> oleCtrl;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&oleCtrl));
}

TEST_F(ComInterfaceTest, QI_IViewObject2) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IViewObject2> viewObj;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&viewObj));
}

TEST_F(ComInterfaceTest, QI_IPersistStreamInit) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IPersistStreamInit> persist;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&persist));
}

TEST_F(ComInterfaceTest, QI_IConnectionPointContainer) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IConnectionPointContainer> cpc;
    EXPECT_HRESULT_SUCCEEDED(control_.QueryInterface(&cpc));
}

TEST_F(ComInterfaceTest, IPersistStreamInit_InitNew) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IPersistStreamInit> persist;
    ASSERT_HRESULT_SUCCEEDED(control_.QueryInterface(&persist));
    EXPECT_HRESULT_SUCCEEDED(persist->InitNew());
}

TEST_F(ComInterfaceTest, IPersistStreamInit_GetClassID) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IPersistStreamInit> persist;
    ASSERT_HRESULT_SUCCEEDED(control_.QueryInterface(&persist));
    CLSID clsid;
    ASSERT_HRESULT_SUCCEEDED(persist->GetClassID(&clsid));
    EXPECT_EQ(clsid, CLSID_PivoxControl);
}

TEST_F(ComInterfaceTest, ConnectionPoint_Enumeration) {
    ASSERT_HRESULT_SUCCEEDED(CreateControl());
    ATL::CComPtr<IConnectionPointContainer> cpc;
    ASSERT_HRESULT_SUCCEEDED(control_.QueryInterface(&cpc));
    ATL::CComPtr<IEnumConnectionPoints> enumCP;
    EXPECT_HRESULT_SUCCEEDED(cpc->EnumConnectionPoints(&enumCP));
}
