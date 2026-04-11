#pragma once

#include "framework.h"

// Win32/WinRT macro collision fix
#ifdef GetCurrentTime
#undef GetCurrentTime
#endif

#include <shlobj.h>

// Bootstrap
#include <WindowsAppSDK-VersionInfo.h>
#include <MddBootstrap.h>

// WinRT headers
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Foundation.Collections.h>
#include <winrt/Windows.UI.Xaml.Interop.h>
#include <winrt/Microsoft.UI.Dispatching.h>
#include <winrt/Microsoft.UI.Xaml.Hosting.h>
#include <winrt/Microsoft.UI.Xaml.Controls.h>
#include <winrt/Microsoft.UI.Xaml.Controls.Primitives.h>
#include <winrt/Microsoft.UI.Xaml.Input.h>
#include <winrt/Microsoft.UI.Xaml.Markup.h>
#include <winrt/Microsoft.UI.Xaml.Media.h>
#include <winrt/Microsoft.UI.Xaml.Shapes.h>
#include <winrt/Microsoft.UI.Content.h>
#include <winrt/Microsoft.UI.Interop.h>
#include <winrt/Microsoft.UI.h>
#include <winrt/Microsoft.Windows.ApplicationModel.Resources.h>
#include <winrt/Windows.ApplicationModel.DataTransfer.h>
#include <winrt/Microsoft.UI.Xaml.Navigation.h>
