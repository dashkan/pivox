using System;
using System.Diagnostics;
using System.IO;
using System.Threading;
using FlaUI.Core;
using FlaUI.Core.AutomationElements;
using FlaUI.Core.Conditions;
using FlaUI.UIA3;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace PivoxUITests;

[TestClass]
public class TestBase
{
    protected Application App { get; set; } = null!;
    protected Window MainWindow { get; set; } = null!;
    protected UIA3Automation Automation { get; set; } = null!;

    private static string FindPivoxExe()
    {
        var dir = AppContext.BaseDirectory;
        for (int i = 0; i < 10; i++)
        {
            var candidate = Path.Combine(dir, "build-win-x64", "Release", "Pivox.exe");
            if (File.Exists(candidate)) return candidate;
            dir = Path.GetDirectoryName(dir) ?? dir;
        }

        var fallback = @"D:\pivox\native\build-win-x64\Release\Pivox.exe";
        if (File.Exists(fallback)) return fallback;

        throw new FileNotFoundException("Pivox.exe not found. Build the project first.");
    }

    [TestInitialize]
    public void SetUp()
    {
        Automation = new UIA3Automation();
        var exePath = FindPivoxExe();
        var psi = new ProcessStartInfo(exePath) { UseShellExecute = false };
        psi.EnvironmentVariables["USE_AUTH_EMULATOR"] = "1";
        psi.EnvironmentVariables["RESET_AUTH"] = "1";
        psi.EnvironmentVariables["RESET_PREFS"] = "1";
        App = Application.Launch(psi);
        MainWindow = App.GetMainWindow(Automation, TimeSpan.FromSeconds(10));
        Assert.IsNotNull(MainWindow, "Main window did not appear within timeout.");
    }

    [TestCleanup]
    public void TearDown()
    {
        try { App?.Close(); } catch { }
        if (App != null && !App.HasExited)
        {
            try { App.Kill(); } catch { }
        }
        Automation?.Dispose();
    }

    /// <summary>Relaunch the app with specific env vars.</summary>
    protected void Relaunch(bool resetAuth = true, bool resetPrefs = false)
    {
        try { App?.Close(); } catch { }
        if (App != null && !App.HasExited)
        {
            try { App.Kill(); } catch { }
        }
        Thread.Sleep(500);

        var exePath = FindPivoxExe();
        var psi = new ProcessStartInfo(exePath) { UseShellExecute = false };
        psi.EnvironmentVariables["USE_AUTH_EMULATOR"] = "1";
        if (resetAuth) psi.EnvironmentVariables["RESET_AUTH"] = "1";
        if (resetPrefs) psi.EnvironmentVariables["RESET_PREFS"] = "1";
        App = Application.Launch(psi);
        MainWindow = App.GetMainWindow(Automation, TimeSpan.FromSeconds(10));
        Assert.IsNotNull(MainWindow, "Main window did not appear after relaunch.");
    }

    /// <summary>Find element by AutomationId with timeout.</summary>
    protected AutomationElement? FindById(string automationId, int timeoutMs = 3000)
    {
        var deadline = DateTime.Now.AddMilliseconds(timeoutMs);
        while (DateTime.Now < deadline)
        {
            var el = MainWindow?.FindFirstDescendant(cf => cf.ByAutomationId(automationId));
            if (el != null) return el;
            Thread.Sleep(100);
        }
        return null;
    }

    /// <summary>Wait for element to have non-empty text content.</summary>
    protected bool WaitForText(string automationId, int timeoutMs = 5000)
    {
        var deadline = DateTime.Now.AddMilliseconds(timeoutMs);
        while (DateTime.Now < deadline)
        {
            var el = MainWindow?.FindFirstDescendant(cf => cf.ByAutomationId(automationId));
            if (el != null)
            {
                var text = el.Name ?? "";
                if (string.IsNullOrEmpty(text))
                {
                    try { text = el.AsTextBox().Text ?? ""; } catch { }
                }
                if (!string.IsNullOrWhiteSpace(text) && text != " ") return true;
            }
            Thread.Sleep(100);
        }
        return false;
    }

    /// <summary>Generate unique email for test isolation.</summary>
    protected static string UniqueEmail(string prefix = "test")
        => $"{prefix}-{Guid.NewGuid().ToString("N")[..8]}@pivox.app";

    /// <summary>Navigate from login to register page.</summary>
    protected void GoToRegister()
    {
        var link = FindById("login-switch-register", 5000);
        Assert.IsNotNull(link, "Switch to register link not found");

        // Use Invoke pattern if available (more reliable for HyperlinkButton),
        // fall back to Click.
        if (link.Patterns.Invoke.IsSupported)
        {
            link.Patterns.Invoke.Pattern.Invoke();
        }
        else
        {
            link.Click();
        }

        // Wait for register page to load.
        var emailBox = FindById("register-email", 5000);
        Assert.IsNotNull(emailBox, "Register page did not load — register-email not found");
    }

    /// <summary>Register an account through the UI. Returns the email used.</summary>
    protected string RegisterAccount(string? email = null, string password = "Testpass123!",
        string displayName = "Test User")
    {
        email ??= UniqueEmail();

        GoToRegister();

        var emailBox = FindById("register-email");
        Assert.IsNotNull(emailBox, "register-email not found");
        emailBox.AsTextBox().Enter(email);

        var nameBox = FindById("register-display-name");
        Assert.IsNotNull(nameBox, "register-display-name not found");
        nameBox.AsTextBox().Enter(displayName);

        var passBox = FindById("register-password");
        Assert.IsNotNull(passBox, "register-password not found");
        passBox.AsTextBox().Enter(password);

        var confirmBox = FindById("register-confirm-password");
        Assert.IsNotNull(confirmBox, "register-confirm-password not found");
        confirmBox.AsTextBox().Enter(password);

        var createBtn = FindById("register-create-account");
        Assert.IsNotNull(createBtn, "register-create-account not found");
        createBtn.Click();

        Thread.Sleep(2000);
        return email;
    }

    /// <summary>Sign in through the login UI.</summary>
    protected void SignIn(string email, string password)
    {
        var emailBox = FindById("login-email");
        Assert.IsNotNull(emailBox, "login-email not found");
        emailBox.AsTextBox().Enter(email);

        var passBox = FindById("login-password");
        Assert.IsNotNull(passBox, "login-password not found");
        passBox.AsTextBox().Enter(password);

        var signInBtn = FindById("login-sign-in");
        Assert.IsNotNull(signInBtn, "login-sign-in not found");
        signInBtn.Click();

        Thread.Sleep(2000);
    }

    /// <summary>Sign out via the profile sidebar.</summary>
    protected void SignOut()
    {
        var profileNav = FindById("nav-profile");
        Assert.IsNotNull(profileNav, "nav-profile not found");
        profileNav.Click();
        Thread.Sleep(500);

        var signOutBtn = FindById("profile-sign-out");
        Assert.IsNotNull(signOutBtn, "profile-sign-out not found");
        signOutBtn.Click();
        Thread.Sleep(1000);
    }
}
