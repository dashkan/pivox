#include "pch.h"
#include "DragSource.h"

namespace pivox {

void DragSource::AttachToContent(const winrt::Microsoft::UI::Xaml::UIElement& element) {
    Detach();
    attachedElement_ = element;

    dragStartingToken_ = element.DragStarting(
        [this](const auto& sender, const auto& args) {
            OnDragStarting(sender, args);
        });
}

void DragSource::Detach() {
    if (attachedElement_) {
        attachedElement_.DragStarting(dragStartingToken_);
        attachedElement_ = nullptr;
    }
}

void DragSource::OnDragStarting(
    const winrt::Microsoft::UI::Xaml::UIElement& /*sender*/,
    const winrt::Microsoft::UI::Xaml::DragStartingEventArgs& args)
{
    // Set custom clipboard format data on the DataPackage.
    // The host application reads these formats from the IDataObject
    // produced by the drag operation.
    auto dataPackage = args.Data();
    dataPackage.RequestedOperation(
        winrt::Windows::ApplicationModel::DataTransfer::DataPackageOperation::Copy);

    // Custom format: application-specific drag payload.
    // Format strings are registered by WinRT automatically.
    dataPackage.SetData(L"application/x-pivox-drag",
        winrt::Windows::Foundation::PropertyValue::CreateString(L""));
}

} // namespace pivox
