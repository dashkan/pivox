using Microsoft.UI.Xaml;
using Pivox.Auth;
using Pivox.Client;
using Pivox.Shared.Auth;

namespace Pivox;

/// <summary>
/// Composition root. Creates the single instances of
/// <see cref="WindowsAuthService"/> and <see cref="PivoxClient"/>
/// that live for the process lifetime, then hands them to MainWindow.
/// Mirrors the macOS AppDelegate's wiring.
/// </summary>
public partial class App : Application
{
    private Window? _window;
    private WindowsAuthService? _auth;
    private PivoxClient? _pivox;

    public App()
    {
        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        _auth = new WindowsAuthService();
        _pivox = new PivoxClient(_auth);

        _window = new MainWindow(_auth, _pivox);
        _window.Activate();
    }
}
