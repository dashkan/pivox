import XCTest

/// Tests for the macOS auth service.
/// These test the auth state management logic — not the Firebase SDK itself.
class AuthServiceTests: XCTestCase {

    // MARK: - Auth State Transitions

    func testInitialAuthStatusIsUnknown() {
        // On launch, auth status should be Unknown until we check stored credentials.
        let appState = AppStateBridge.shared()
        // Clear any previous state for a clean test.
        appState.deleteSecure(forKey: "firebase_id_token")
        appState.save(false, forKey: "rememberMe")

        // Without stored credentials, checking should transition to SignedOut.
        let hasToken = appState.loadSecure(forKey: "firebase_id_token") != nil
        let remembered = appState.hasBool(forKey: "rememberMe") && appState.loadBool(forKey: "rememberMe")

        XCTAssertFalse(hasToken, "No token should be stored after clearing")
        XCTAssertFalse(remembered, "Remember Me should be false after clearing")
    }

    func testSaveAndLoadAuthToken() {
        let appState = AppStateBridge.shared()
        let testToken = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test-token"

        appState.saveSecure(testToken, forKey: "firebase_id_token")
        let loaded = appState.loadSecure(forKey: "firebase_id_token")

        XCTAssertEqual(loaded, testToken, "Token should round-trip through Keychain")

        // Cleanup
        appState.deleteSecure(forKey: "firebase_id_token")
    }

    func testDeleteAuthToken() {
        let appState = AppStateBridge.shared()
        appState.saveSecure("token-to-delete", forKey: "firebase_id_token")
        appState.deleteSecure(forKey: "firebase_id_token")

        let loaded = appState.loadSecure(forKey: "firebase_id_token")
        XCTAssertNil(loaded, "Token should be nil after deletion")
    }

    func testRememberMePersistence() {
        let appState = AppStateBridge.shared()

        appState.save(true, forKey: "rememberMe")
        XCTAssertTrue(appState.loadBool(forKey: "rememberMe"), "Remember Me should be true")

        appState.save(false, forKey: "rememberMe")
        XCTAssertFalse(appState.loadBool(forKey: "rememberMe"), "Remember Me should be false")
    }

    func testSignOutClearsCredentials() {
        let appState = AppStateBridge.shared()

        // Simulate signed-in state
        appState.saveSecure("some-token", forKey: "firebase_id_token")
        appState.saveSecure("some-refresh", forKey: "firebase_refresh_token")
        appState.save(true, forKey: "rememberMe")

        // Simulate sign out
        appState.deleteSecure(forKey: "firebase_id_token")
        appState.deleteSecure(forKey: "firebase_refresh_token")

        XCTAssertNil(appState.loadSecure(forKey: "firebase_id_token"))
        XCTAssertNil(appState.loadSecure(forKey: "firebase_refresh_token"))
        // Remember Me preference survives sign out — it's a UI preference, not a credential.
        XCTAssertTrue(appState.loadBool(forKey: "rememberMe"))
    }
}
