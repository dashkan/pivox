#pragma once

#include "framework.h"

// WinRT headers for XAML Islands hosting.
#include <windows.h>
#undef GetCurrentTime  // Win32 macro collides with WinRT

#include <unknwn.h>
#include <hstring.h>

#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Foundation.Collections.h>
#include <winrt/Windows.UI.Xaml.Interop.h>
#include <winrt/Microsoft.UI.Composition.h>
#include <winrt/Microsoft.UI.Content.h>
#include <winrt/Microsoft.UI.Dispatching.h>
#include <winrt/Microsoft.UI.Interop.h>
#include <winrt/Microsoft.UI.Windowing.h>
#include <winrt/Microsoft.UI.Xaml.h>
#include <winrt/Microsoft.UI.Xaml.Controls.h>
#include <winrt/Microsoft.UI.Xaml.Controls.Primitives.h>
#include <winrt/Microsoft.UI.Xaml.Data.h>
#include <winrt/Microsoft.UI.Xaml.Hosting.h>
#include <winrt/Microsoft.UI.Xaml.Input.h>
#include <winrt/Microsoft.UI.Xaml.Markup.h>
#include <winrt/Microsoft.UI.Xaml.Media.h>
#include <winrt/Microsoft.UI.Xaml.Navigation.h>
#include <winrt/Microsoft.UI.Xaml.Shapes.h>
#include <winrt/Microsoft.UI.h>

// XAML Islands interop
#include <windows.ui.xaml.hosting.desktopwindowxamlsource.h>

// Drag source
#include <winrt/Windows.ApplicationModel.DataTransfer.h>

// Windows App SDK deployment
#include <winrt/Microsoft.Windows.ApplicationModel.WindowsAppRuntime.h>
