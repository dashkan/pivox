#include "pch.h"
#include "MainWindow.xaml.h"
#include "MainWindow.g.cpp"
#include "LoginPage.xaml.h"

namespace winrt::Pivox::implementation
{
    MainWindow::MainWindow()
    {
        InitializeComponent();
        SetupWindow();
        ShowAuth();
    }

    void MainWindow::SetupWindow()
    {
        // Set window size via AppWindow.
        if (auto appWindow = this->AppWindow())
        {
            appWindow.Resize({ 1280, 800 });

            // Set minimum size isn't directly available, but we set a reasonable default.
            appWindow.Title(L"Pivox");
        }
    }

    void MainWindow::ShowAuth()
    {
        AuthContainer().Visibility(Microsoft::UI::Xaml::Visibility::Visible);
        MainContainer().Visibility(Microsoft::UI::Xaml::Visibility::Collapsed);
        AuthFrame().Navigate(winrt::xaml_typename<Pivox::LoginPage>());
    }

    void MainWindow::ShowMainApp()
    {
        AuthContainer().Visibility(Microsoft::UI::Xaml::Visibility::Collapsed);
        MainContainer().Visibility(Microsoft::UI::Xaml::Visibility::Visible);

        // Select the first nav item (Operator).
        if (NavView().MenuItems().Size() > 0)
        {
            NavView().SelectedItem(NavView().MenuItems().GetAt(0));
        }
    }

    void MainWindow::OnNavSelectionChanged(
        Microsoft::UI::Xaml::Controls::NavigationView const&,
        Microsoft::UI::Xaml::Controls::NavigationViewSelectionChangedEventArgs const& args)
    {
        if (auto selectedItem = args.SelectedItem())
        {
            auto navItem = selectedItem.as<Microsoft::UI::Xaml::Controls::NavigationViewItem>();
            auto tag = winrt::unbox_value<winrt::hstring>(navItem.Tag());

            if (tag == L"Profile")
            {
                ContentPanel().Visibility(Microsoft::UI::Xaml::Visibility::Collapsed);
                ProfilePanel().Visibility(Microsoft::UI::Xaml::Visibility::Visible);
            }
            else
            {
                ContentPanel().Visibility(Microsoft::UI::Xaml::Visibility::Visible);
                ProfilePanel().Visibility(Microsoft::UI::Xaml::Visibility::Collapsed);
                SectionTitle().Text(tag);
            }
        }
    }

    void MainWindow::OnMenuExit(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        this->Close();
    }

    void MainWindow::OnMenuToggleSidebar(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        NavView().IsPaneOpen(!NavView().IsPaneOpen());
    }

    void MainWindow::OnMenuMinimize(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        if (auto presenter = this->AppWindow().Presenter().try_as<Microsoft::UI::Windowing::OverlappedPresenter>())
        {
            presenter.Minimize();
        }
    }

    void MainWindow::OnMenuMaximize(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        if (auto presenter = this->AppWindow().Presenter().try_as<Microsoft::UI::Windowing::OverlappedPresenter>())
        {
            if (presenter.State() == Microsoft::UI::Windowing::OverlappedPresenterState::Maximized)
            {
                presenter.Restore();
            }
            else
            {
                presenter.Maximize();
            }
        }
    }

    void MainWindow::OnSignOut(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        ShowAuth();
    }
}
