using System;
using System.Diagnostics;
using System.IO;
using FlaUI.Core;
using FlaUI.UIA3;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace PivoxUITests;

/// <summary>
/// Base class for Pivox UI tests. Launches the app with RESET_AUTH=1
/// to ensure a clean login screen, and cleans up on teardown.
/// </summary>
public class TestBase
{
    protected Application? App { get; set; }
    protected UIA3Automation? Automation { get; set; }
    protected FlaUI.Core.AutomationElements.Window? MainWindow { get; set; }

    private static string FindPivoxExe()
    {
        // Walk up from test assembly to find the build output.
        var dir = AppContext.BaseDirectory;
        for (int i = 0; i < 8; i++)
        {
            var candidate = Path.Combine(dir, "build-win-x64", "Release", "Pivox.exe");
            if (File.Exists(candidate)) return candidate;
            dir = Path.GetDirectoryName(dir) ?? dir;
        }

        // Try absolute path as fallback.
        var fallback = @"D:\pivox\native\build-win-x64\Release\Pivox.exe";
        if (File.Exists(fallback)) return fallback;

        throw new FileNotFoundException("Pivox.exe not found. Build the project first.");
    }

    [TestInitialize]
    public void SetUp()
    {
        var exePath = FindPivoxExe();

        var startInfo = new ProcessStartInfo(exePath)
        {
            UseShellExecute = false,
        };
        startInfo.Environment["RESET_AUTH"] = "1";

        App = Application.Launch(startInfo);
        Automation = new UIA3Automation();

        // Wait for the main window to appear.
        MainWindow = App.GetMainWindow(Automation, TimeSpan.FromSeconds(10));
        Assert.IsNotNull(MainWindow, "Main window did not appear within timeout.");
    }

    [TestCleanup]
    public void TearDown()
    {
        App?.Close();

        // Give the app a moment to close gracefully.
        if (App != null && !App.HasExited)
        {
            try { App.Kill(); } catch { }
        }

        Automation?.Dispose();
    }

    /// <summary>
    /// Find an element by AutomationId with a timeout.
    /// </summary>
    protected FlaUI.Core.AutomationElements.AutomationElement? FindById(string automationId, int timeoutMs = 3000)
    {
        var deadline = DateTime.Now.AddMilliseconds(timeoutMs);
        while (DateTime.Now < deadline)
        {
            var element = MainWindow?.FindFirstDescendant(cf => cf.ByAutomationId(automationId));
            if (element != null) return element;
            System.Threading.Thread.Sleep(100);
        }
        return null;
    }
}
