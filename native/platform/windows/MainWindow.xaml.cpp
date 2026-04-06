#include "pch.h"
#include "MainWindow.xaml.h"
#include "MainWindow.g.cpp"
#include "LoginPage.xaml.h"
#include "App.xaml.h"

namespace winrt::Pivox::implementation
{
    MainWindow::MainWindow()
    {
        InitializeComponent();
        SetupWindow();

        // Firebase manages session persistence. Check if a user is signed in.
        if (App::AuthService()->hasValidSession())
        {
            ShowMainApp();
        }
        else
        {
            ShowAuth();
        }
    }

    void MainWindow::SetupWindow()
    {
        auto appWindow = this->AppWindow();

        // Restore saved window state, or use defaults.
        auto& appState = App::AppState();
        auto saved = appState->loadWindowState();
        if (saved.has_value())
        {
            appWindow.Resize({ saved->width, saved->height });
            appWindow.Move({ saved->x, saved->y });
        }
        else
        {
            appWindow.Resize({ 1280, 800 });
        }

        appWindow.Title(L"Pivox");

        // Extend content into title bar — seamless dark background like Calculator.
        auto titleBar = appWindow.TitleBar();
        titleBar.ExtendsContentIntoTitleBar(true);
        titleBar.ButtonBackgroundColor(Microsoft::UI::Colors::Transparent());
        titleBar.ButtonInactiveBackgroundColor(Microsoft::UI::Colors::Transparent());

        // Minimum window size — Windows App SDK 1.7 OverlappedPresenter API
        // (microsoft/microsoft-ui-xaml#2945, #7296).
        if (auto presenter = appWindow.Presenter().try_as<Microsoft::UI::Windowing::OverlappedPresenter>())
        {
            presenter.PreferredMinimumWidth(1024);
            presenter.PreferredMinimumHeight(768);
        }

        // Save window state on move/resize.
        m_changedToken = appWindow.Changed(
            [this](Microsoft::UI::Windowing::AppWindow const&,
                   Microsoft::UI::Windowing::AppWindowChangedEventArgs const& args)
            {
                if (args.DidPositionChange() || args.DidSizeChange())
                {
                    SaveWindowState();
                }
            });
    }

    void MainWindow::SaveWindowState()
    {
        auto appWindow = this->AppWindow();
        auto pos = appWindow.Position();
        auto size = appWindow.Size();

        pivox::WindowState ws;
        ws.x = pos.X;
        ws.y = pos.Y;
        ws.width = size.Width;
        ws.height = size.Height;

        App::AppState()->saveWindowState(ws);
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
                // Update profile with current auth user data.
                auto& user = App::AuthService()->currentUser();
                auto displayName = user.displayName.empty() ? "User" : user.displayName;
                auto email = user.email.empty() ? "" : user.email;
                ProfileName().Text(winrt::to_hstring(displayName));
                ProfileEmail().Text(winrt::to_hstring(email));
                ProfileAvatar().DisplayName(winrt::to_hstring(displayName));

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
        App::AuthService()->signOut();
        ShowAuth();
    }
}
