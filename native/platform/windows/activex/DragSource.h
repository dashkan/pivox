#pragma once
#include "pch.h"

namespace pivox {

/// Bridges WinUI DragStarting events to OLE drag-and-drop for the ActiveX host.
/// Source only — never a drop target.
class DragSource {
public:
    /// Register for DragStarting events on XAML content.
    /// Uses DataPackage.SetData(formatId, value) with custom clipboard format strings.
    /// WinRT handles format registration.
    void AttachToContent(const winrt::Microsoft::UI::Xaml::UIElement& element);

    /// Detach all event handlers.
    void Detach();

private:
    void OnDragStarting(
        const winrt::Microsoft::UI::Xaml::UIElement& sender,
        const winrt::Microsoft::UI::Xaml::DragStartingEventArgs& args);

    winrt::event_token dragStartingToken_;
    winrt::Microsoft::UI::Xaml::UIElement attachedElement_{ nullptr };
};

} // namespace pivox
