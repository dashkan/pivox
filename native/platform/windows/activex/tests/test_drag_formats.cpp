#include <gtest/gtest.h>
#include <windows.h>
#include <atlbase.h>
#include <olectl.h>

#include "PivoxActiveX_i.h"

class DragFormatTest : public ::testing::Test {
protected:
    void SetUp() override {
        HRESULT hr = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
        ASSERT_TRUE(SUCCEEDED(hr) || hr == S_FALSE);
    }

    void TearDown() override {
        CoUninitialize();
    }
};

TEST_F(DragFormatTest, CustomClipboardFormatRegistered) {
    // Verify our custom format string can be registered.
    UINT format = RegisterClipboardFormatW(L"application/x-pivox-drag");
    EXPECT_NE(format, 0u);
}

TEST_F(DragFormatTest, CustomFormatPersistentAcrossCalls) {
    // Same format string must return the same ID.
    UINT format1 = RegisterClipboardFormatW(L"application/x-pivox-drag");
    UINT format2 = RegisterClipboardFormatW(L"application/x-pivox-drag");
    EXPECT_EQ(format1, format2);
}

TEST_F(DragFormatTest, CustomFormatCoexistsWithStandardFormats) {
    // Custom format must not collide with standard formats.
    UINT customFormat = RegisterClipboardFormatW(L"application/x-pivox-drag");
    EXPECT_NE(customFormat, static_cast<UINT>(CF_TEXT));
    EXPECT_NE(customFormat, static_cast<UINT>(CF_UNICODETEXT));
    EXPECT_NE(customFormat, static_cast<UINT>(CF_BITMAP));
    EXPECT_NE(customFormat, static_cast<UINT>(CF_HDROP));
}

TEST_F(DragFormatTest, DataObjectSupportsEnumFormatEtc) {
    // Create the control and verify IDataObject is available.
    ATL::CComPtr<IUnknown> control;
    HRESULT hr = control.CoCreateInstance(CLSID_PivoxControl);
    if (FAILED(hr)) {
        GTEST_SKIP() << "Control not registered — skipping DataObject test.";
    }

    ATL::CComPtr<IDataObject> dataObj;
    hr = control.QueryInterface(&dataObj);
    ASSERT_HRESULT_SUCCEEDED(hr);

    ATL::CComPtr<IEnumFORMATETC> enumFmt;
    hr = dataObj->EnumFormatEtc(DATADIR_GET, &enumFmt);
    // May return S_OK or S_FALSE (no formats yet — control not activated).
    EXPECT_TRUE(hr == S_OK || hr == S_FALSE || hr == OLE_S_USEREG);
}
