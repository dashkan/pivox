#pragma once
// DragService — compile-time abstraction for drag-and-drop.
//
// PIVOX_ACTIVEX_HOST: cancels WinUI drag, builds IDataObject from the
// DataPackage, posts to ATL HWND for manual in-process drag.
//
// Default (app): passes through to native WinUI drag (no-op, caller
// already set data on args.Data()).
//
// Usage (identical to native WinUI DragStarting handler):
//
//   element.DragStarting([](auto&&, DragStartingEventArgs const& args) {
//       args.Data().SetText(L"my payload");
//       args.AllowedOperations(DataPackageOperation::Copy);
//       Pivox::DragService::HandleDragStarting(args);
//   });

#ifdef PIVOX_SHARED_EXPORTS
#define PIVOX_API __declspec(dllexport)
#else
#define PIVOX_API __declspec(dllimport)
#endif

namespace Pivox {

class DragService {
public:
    // Call AFTER setting data on args.Data() and args.AllowedOperations().
    // On ActiveX: cancels WinUI drag, builds IDataObject from text, starts manual drag.
    // On App: no-op (native WinUI handles everything).
    // Pass the same text you set on args.Data().SetText() — avoids async
    // extraction from DataPackage which fails STA assertion in ActiveX hosts.
    PIVOX_API static void HandleDragStarting(
        const winrt::Microsoft::UI::Xaml::DragStartingEventArgs& args,
        const winrt::hstring& text);
};

} // namespace Pivox
