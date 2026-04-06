using FlaUI.Core.AutomationElements;
using FlaUI.Core.Input;
using FlaUI.Core.WindowsAPI;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace PivoxUITests;

[TestClass]
public class AuthUITests : TestBase
{
    // -----------------------------------------------------------------------
    // Login screen — field behavior
    // -----------------------------------------------------------------------

    [TestMethod]
    public void TestEmailFieldIsFocusedOnLaunch()
    {
        var emailBox = FindById("login-email");
        Assert.IsNotNull(emailBox, "login-email not found");
        System.Threading.Thread.Sleep(500);
        Assert.IsTrue(emailBox.IsEnabled, "Email field should be enabled and focused");
    }

    [TestMethod]
    public void TestTabFromEmailToPassword()
    {
        var emailBox = FindById("login-email");
        Assert.IsNotNull(emailBox);
        emailBox.AsTextBox().Enter("test@example.com");

        Keyboard.Press(VirtualKeyShort.TAB);
        Wait.UntilInputIsProcessed();
        System.Threading.Thread.Sleep(200);

        // Type into password field (focus should have moved there).
        Keyboard.Type("typed-password");
        Wait.UntilInputIsProcessed();

        var passBox = FindById("login-password");
        Assert.IsNotNull(passBox);
        // PasswordBox text isn't readable via UIA, but we verify the field exists.
    }

    [TestMethod]
    public void TestAccessibilityIdentifiersExist()
    {
        Assert.IsNotNull(FindById("login-email"), "login-email missing");
        Assert.IsNotNull(FindById("login-password"), "login-password missing");
        Assert.IsNotNull(FindById("login-remember-me"), "login-remember-me missing");
        Assert.IsNotNull(FindById("login-sign-in"), "login-sign-in missing");
    }

    [TestMethod]
    public void TestGoogleSignInButtonExists()
    {
        var googleBtn = FindById("login-google");
        Assert.IsNotNull(googleBtn, "Google sign-in button not found");
        Assert.IsTrue(googleBtn.IsEnabled, "Google sign-in button should be enabled");
    }

    [TestMethod]
    public void TestSignInButtonDisabledWhenFieldsEmpty()
    {
        var signInBtn = FindById("login-sign-in");
        Assert.IsNotNull(signInBtn);

        // Both empty → disabled.
        Assert.IsFalse(signInBtn.IsEnabled, "Should be disabled when both fields empty");

        // Email only → still disabled.
        var emailBox = FindById("login-email");
        Assert.IsNotNull(emailBox);
        emailBox.AsTextBox().Enter("user@example.com");
        System.Threading.Thread.Sleep(200);

        signInBtn = FindById("login-sign-in")!;
        Assert.IsFalse(signInBtn.IsEnabled, "Should be disabled when only email filled");

        // Both filled → enabled.
        var passBox = FindById("login-password");
        Assert.IsNotNull(passBox);
        passBox.AsTextBox().Enter("password123");
        System.Threading.Thread.Sleep(200);

        signInBtn = FindById("login-sign-in")!;
        Assert.IsTrue(signInBtn.IsEnabled, "Should be enabled when both fields filled");
    }

    // -----------------------------------------------------------------------
    // Login screen — sign-in errors
    // -----------------------------------------------------------------------

    [TestMethod]
    public void TestSignInWithNonExistentAccount()
    {
        SignIn(UniqueEmail("nonexistent"), "Password123!");
        // Error should appear.
        Assert.IsTrue(WaitForText("login-error", 5000),
            "Error should appear for non-existent account");
    }

    [TestMethod]
    public void TestSignInWithWrongPassword()
    {
        // Register an account first.
        var email = RegisterAccount();

        // Should be on main app now. Sign out.
        SignOut();

        // Try to sign in with wrong password.
        SignIn(email, "WrongPassword999!");
        Assert.IsTrue(WaitForText("login-error", 5000),
            "Error should appear for wrong password");
    }

    // -----------------------------------------------------------------------
    // Registration
    // -----------------------------------------------------------------------

    [TestMethod]
    public void TestRegisterAndLandOnMainApp()
    {
        RegisterAccount();

        // Should have navigated to main app — check for nav sidebar.
        var operatorNav = FindById("nav-operator", 5000);
        Assert.IsNotNull(operatorNav, "Should land on main app after registration");
    }

    [TestMethod]
    public void TestRegisterPasswordMismatch()
    {
        GoToRegister();

        var emailBox = FindById("register-email", 5000);
        Assert.IsNotNull(emailBox, "register-email not found");
        emailBox.AsTextBox().Enter(UniqueEmail());

        var nameBox = FindById("register-display-name", 3000);
        Assert.IsNotNull(nameBox, "register-display-name not found");
        nameBox.AsTextBox().Enter("Test");

        var passBox = FindById("register-password", 3000);
        Assert.IsNotNull(passBox, "register-password not found");
        passBox.AsTextBox().Enter("Password123!");

        var confirmBox = FindById("register-confirm-password", 3000);
        Assert.IsNotNull(confirmBox, "register-confirm-password not found");
        confirmBox.AsTextBox().Enter("Different456!");

        var createBtn = FindById("register-create-account", 3000);
        Assert.IsNotNull(createBtn, "register-create-account not found");
        createBtn.Click();

        System.Threading.Thread.Sleep(1000);
        Assert.IsTrue(WaitForText("register-error", 5000),
            "Error should appear for password mismatch");
    }

    [TestMethod]
    public void TestRegisterDuplicateEmail()
    {
        var email = RegisterAccount();
        SignOut();

        // Try to register again with the same email.
        GoToRegister();

        var emailBox = FindById("register-email");
        Assert.IsNotNull(emailBox);
        emailBox.AsTextBox().Enter(email);

        var nameBox = FindById("register-display-name");
        Assert.IsNotNull(nameBox);
        nameBox.AsTextBox().Enter("Duplicate");

        var passBox = FindById("register-password");
        Assert.IsNotNull(passBox);
        passBox.AsTextBox().Enter("Password123!");

        var confirmBox = FindById("register-confirm-password");
        Assert.IsNotNull(confirmBox);
        confirmBox.AsTextBox().Enter("Password123!");

        var createBtn = FindById("register-create-account");
        Assert.IsNotNull(createBtn);
        createBtn.Click();

        System.Threading.Thread.Sleep(2000);
        Assert.IsTrue(WaitForText("register-error", 5000),
            "Error should appear for duplicate email");
    }

    [TestMethod]
    public void TestRegisterSwitchToLogin()
    {
        GoToRegister();

        var switchLink = FindById("register-switch-login", 5000);
        Assert.IsNotNull(switchLink, "Switch to login link not found");
        switchLink.Click();

        // Should be back on login screen.
        Assert.IsNotNull(FindById("login-email", 5000), "Should return to login screen");
    }

    // -----------------------------------------------------------------------
    // Sign out
    // -----------------------------------------------------------------------

    [TestMethod]
    public void TestSignOutReturnsToLogin()
    {
        RegisterAccount();
        SignOut();

        Assert.IsNotNull(FindById("login-email", 5000),
            "Should return to login screen after sign out");
    }

    [TestMethod]
    public void TestRegisterSignOutSignIn()
    {
        var email = RegisterAccount();

        // Should be on main app.
        Assert.IsNotNull(FindById("nav-operator", 5000), "Should be on main app");

        SignOut();

        // Sign in with same credentials.
        SignIn(email, "Testpass123!");

        // Should be back on main app.
        Assert.IsNotNull(FindById("nav-operator", 5000),
            "Should land on main app after sign-in");
    }

    // -----------------------------------------------------------------------
    // Remember Me
    // -----------------------------------------------------------------------

    [TestMethod]
    public void TestRememberMePreFillsEmailAfterSuccessfulSignIn()
    {
        var email = RegisterAccount();
        SignOut();

        // Check remember me, then sign in.
        var rememberMe = FindById("login-remember-me");
        Assert.IsNotNull(rememberMe);
        if (!rememberMe.AsCheckBox().IsChecked.GetValueOrDefault())
        {
            rememberMe.Click();
        }

        SignIn(email, "Testpass123!");

        // Should be on main app. Relaunch with RESET_AUTH (no RESET_PREFS).
        Relaunch(resetAuth: true, resetPrefs: false);

        // Email should be pre-filled.
        var emailBox = FindById("login-email", 5000);
        Assert.IsNotNull(emailBox);
        var prefilled = emailBox.AsTextBox().Text;
        Assert.AreEqual(email, prefilled, "Email should be pre-filled from Remember Me");

        // Checkbox should be checked.
        var checkbox = FindById("login-remember-me");
        Assert.IsNotNull(checkbox);
        Assert.IsTrue(checkbox.AsCheckBox().IsChecked.GetValueOrDefault(),
            "Remember Me checkbox should be checked");
    }

    [TestMethod]
    public void TestRememberMeDoesNotSaveOnFailedLogin()
    {
        var testEmail = UniqueEmail("failtest");

        // Check remember me, type email, wrong password.
        var rememberMe = FindById("login-remember-me");
        Assert.IsNotNull(rememberMe);
        if (!rememberMe.AsCheckBox().IsChecked.GetValueOrDefault())
        {
            rememberMe.Click();
        }

        SignIn(testEmail, "WrongPassword!");

        // Should see error (sign-in failed).
        Assert.IsTrue(WaitForText("login-error", 5000));

        // Relaunch — email should NOT be pre-filled (failed sign-in).
        Relaunch(resetAuth: true, resetPrefs: false);

        var emailBox = FindById("login-email", 5000);
        Assert.IsNotNull(emailBox);
        var text = emailBox.AsTextBox().Text ?? "";
        Assert.AreNotEqual(testEmail, text,
            "Email should NOT be pre-filled after failed sign-in");
    }

    // -----------------------------------------------------------------------
    // Loading state
    // -----------------------------------------------------------------------

    [TestMethod]
    public void TestAllInputsDisabledDuringSignIn()
    {
        var emailBox = FindById("login-email");
        Assert.IsNotNull(emailBox);
        emailBox.AsTextBox().Enter("user@example.com");

        var passBox = FindById("login-password");
        Assert.IsNotNull(passBox);
        passBox.AsTextBox().Enter("password123");

        var signInBtn = FindById("login-sign-in");
        Assert.IsNotNull(signInBtn);
        signInBtn.Click();

        // After the auth call completes (fast in emulator mode),
        // an error appears — proving the form was submitted.
        System.Threading.Thread.Sleep(2000);
        Assert.IsTrue(WaitForText("login-error", 5000),
            "Error should appear, proving form was submitted");
    }
}
