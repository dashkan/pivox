#include "pch.h"
#include "MainWindow.xaml.h"
#include "MainWindow.g.cpp"
#include "PivoxServices.h"

namespace winrt::PivoxApp::implementation
{
    MainWindow::MainWindow()
    {
        InitializeComponent();
        SetupWindow();

        if (pivox::PivoxServices::authService()->hasValidSession())
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

        auto& appState = pivox::PivoxServices::appState();
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

        auto titleBar = appWindow.TitleBar();
        titleBar.ExtendsContentIntoTitleBar(true);
        titleBar.ButtonBackgroundColor(Microsoft::UI::Colors::Transparent());
        titleBar.ButtonInactiveBackgroundColor(Microsoft::UI::Colors::Transparent());

        if (auto presenter = appWindow.Presenter().try_as<Microsoft::UI::Windowing::OverlappedPresenter>())
        {
            presenter.PreferredMinimumWidth(1024);
            presenter.PreferredMinimumHeight(768);
        }

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

        pivox::PivoxServices::appState()->saveWindowState(ws);
    }

    void MainWindow::ShowAuth()
    {
        AuthContainer().Visibility(Microsoft::UI::Xaml::Visibility::Visible);
        MainContainer().Visibility(Microsoft::UI::Xaml::Visibility::Collapsed);
        AuthFrame().Navigate(winrt::xaml_typename<winrt::Pivox::LoginPage>());
    }

    void MainWindow::ShowMainApp()
    {
        AuthContainer().Visibility(Microsoft::UI::Xaml::Visibility::Collapsed);
        MainContainer().Visibility(Microsoft::UI::Xaml::Visibility::Visible);
        MainFrame().Navigate(winrt::xaml_typename<winrt::Pivox::MainPage>());
    }

    void MainWindow::OnMenuExit(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        this->Close();
    }

    void MainWindow::OnMenuToggleSidebar(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        // MainPage owns the NavigationView — toggle via the page if needed.
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
}
