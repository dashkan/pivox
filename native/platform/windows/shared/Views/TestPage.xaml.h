#pragma once
#include "TestPage.g.h"

namespace winrt::Pivox::implementation
{
    struct TestPage : TestPageT<TestPage>
    {
        TestPage() { InitializeComponent(); }
    };
}

namespace winrt::Pivox::factory_implementation
{
    struct TestPage : TestPageT<TestPage, implementation::TestPage>
    {
    };
}
