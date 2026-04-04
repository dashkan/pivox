# Testing Strategy

## Principles

- **TDD.** Write tests first, then implementation. No exceptions for new code.
- **Native frameworks for native code.** Each platform uses its own test tooling. No cross-platform test abstraction layers.
- **Shared logic tested once.** The shared C++ core runs Google Test on both platforms — same tests, same results. No duplication.
- **Tests run before every commit.** CI enforces this. Local dev should too.

## Framework Reference

### Go (Cloud Controller, Playout Agent)

**Framework:** `go test` (standard library)
**Style:** Table-driven tests
**Mocking:** Interfaces + test doubles (no mocking framework needed for most cases)
**Run:** `go test ./...`

```go
func TestPlayoutController_ResolveElement(t *testing.T) {
    tests := []struct {
        name    string
        element string
        wantCh  int
        wantLy  int
    }{
        {"lower third", "lower-third", 0, 2},
        {"bug", "bug", 0, 1},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

### Rust (Engine)

**Framework:** `cargo test` (built-in)
**Style:** `#[cfg(test)]` modules in each file, integration tests in `tests/` directory
**Mocking:** Trait-based test doubles
**Run:** `cargo test`

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn compositor_transparent_canvas() {
        let compositor = Compositor::new(1920, 1080);
        let frame = compositor.render(&[]);
        assert!(frame.pixels().iter().all(|&p| p == 0));
    }
}
```

### C++ (Shared Core, CEF Bridge, Shared Custom UI)

**Framework:** Google Test (gtest) + Google Mock (gmock)
**Integration:** CMake `FetchContent` — no manual install
**Run:** `ctest` or direct binary execution

```cpp
TEST(AuthManagerTest, SignInWithEmailSuccess) {
    MockFirebaseAuth mock_auth;
    EXPECT_CALL(mock_auth, SignInWithEmailAndPassword("user@test.com", "pass123"))
        .WillOnce(Return(AuthResult::Success(mock_user)));

    AuthManager auth(&mock_auth);
    auto result = auth.signIn("user@test.com", "pass123");
    EXPECT_TRUE(result.ok());
}
```

Google Test runs identically on macOS and Windows. This is where the real cross-platform test unification happens — the shared C++ core contains most of the business logic.

### Swift / Obj-C (macOS Native Layer)

**Unit tests:** XCTest
**UI tests:** XCUITest
**Run:** `xcodebuild test` or Xcode Test Navigator

#### Unit Tests (XCTest)

Test SwiftUI view models, state management, and bindings:

```swift
final class ChannelMonitorViewModelTests: XCTestCase {
    func testLayerStatusUpdatesOnGRPCEvent() {
        let viewModel = ChannelMonitorViewModel()
        viewModel.handleStatusUpdate(ChannelStatus(
            channelId: 0,
            layers: [LayerStatus(id: 0, state: .playing)]
        ))
        XCTAssertEqual(viewModel.layers[0].state, .playing)
    }
}
```

#### UI Tests (XCUITest)

Test workflows by launching the app and interacting via accessibility identifiers:

```swift
final class OperatorWorkflowUITests: XCTestCase {
    let app = XCUIApplication()

    override func setUp() {
        app.launch()
    }

    func testSwitchToDesignerWorkspace() {
        app.buttons["workspace-switcher"].tap()
        app.buttons["designer-mode"].tap()
        XCTAssertTrue(app.staticTexts["Design Canvas"].exists)
    }
}
```

Set accessibility identifiers on all interactive elements in SwiftUI views:

```swift
Button("Designer") { ... }
    .accessibilityIdentifier("designer-mode")
```

### WinUI 3 / C++/WinRT (Windows Native Layer)

**Unit tests:** MSTest (C++/WinRT) or Google Test (for non-XAML C++ code)
**UI tests:** WinAppDriver (Appium protocol)
**Run:** `vstest.console.exe` for unit tests, Appium client for UI tests

#### Unit Tests

For C++/WinRT view models and state — use MSTest with the XAML test host:

```cpp
TEST_CLASS(ChannelMonitorViewModelTests) {
    TEST_METHOD(LayerStatusUpdatesOnGRPCEvent) {
        auto viewModel = winrt::make<ChannelMonitorViewModel>();
        viewModel.HandleStatusUpdate(/* ... */);
        Assert::AreEqual(viewModel.Layers().GetAt(0).State(), LayerState::Playing);
    }
};
```

For logic that doesn't touch XAML (most of it — lives in the shared C++ core), use Google Test directly. No XAML runtime needed.

#### UI Tests (WinAppDriver)

WinAppDriver uses the Appium protocol. Test code can be written in any language with an Appium client (C#, Python, etc.):

```python
# Python with Appium client
def test_switch_to_designer_workspace(self):
    workspace_switcher = self.driver.find_element(By.ACCESSIBILITY_ID, "workspace-switcher")
    workspace_switcher.click()
    designer_button = self.driver.find_element(By.ACCESSIBILITY_ID, "designer-mode")
    designer_button.click()
    canvas = self.driver.find_element(By.ACCESSIBILITY_ID, "design-canvas")
    assert canvas.is_displayed()
```

Set `AutomationProperties.AutomationId` on all interactive XAML elements:

```xml
<Button x:Name="DesignerMode"
        AutomationProperties.AutomationId="designer-mode" />
```

### End-to-End Tests

E2E tests verify complete workflows across the full stack: native app → Cloud Controller / Playout Agent → engine.

#### Native App E2E

**macOS:** XCUITest driving the full app against a local Playout Agent + embedded engine.
**Windows:** WinAppDriver driving the full app against a local Playout Agent + embedded engine.

These tests launch the real app, perform operator workflows (load rundown, cue item, take, verify preview output), and assert on outcomes. They're slow, run in CI, and cover the critical paths.

#### Engine E2E

A gRPC test client (Go or Rust) sends playout commands to a running engine and verifies output:

```go
func TestEngine_PlayGraphicOverVideo(t *testing.T) {
    conn := connectToEngine(t)
    client := proto.NewPlayoutClient(conn)

    // Load video on layer 0
    _, err := client.VideoLoad(ctx, &proto.VideoLoadCommand{
        Channel: 0, Layer: 0, Path: "testdata/reference.mxf",
    })
    require.NoError(t, err)

    // Load graphic on layer 1
    _, err = client.Load(ctx, &proto.LoadCommand{
        Channel: 0, Layer: 1, Template: "testdata/lower-third.html",
    })
    require.NoError(t, err)

    // Take and capture NDI output frame
    _, err = client.Play(ctx, &proto.PlayCommand{Channel: 0, Layer: 1})
    require.NoError(t, err)

    frame := captureNDIFrame(t, "PIVOX-CH0")
    assertFrameMatchesReference(t, frame, "testdata/expected_composite.png", tolerance)
}
```

#### API E2E

Test the Cloud Controller and Playout Agent APIs end-to-end:

```go
func TestRundownWorkflow(t *testing.T) {
    // Create show
    show := createShow(t, "Test Show")

    // Create rundown with items
    rundown := createRundown(t, show.ID)
    addItem(t, rundown.ID, "lower-third", templateID, data)

    // Cue first item
    cue(t, rundown.ID, 0)
    status := getChannelStatus(t, 0)
    require.Equal(t, "cued", status.Layers[2].BackgroundState)

    // Take
    take(t, rundown.ID, 0)
    status = getChannelStatus(t, 0)
    require.Equal(t, "playing", status.Layers[2].ForegroundState)
}
```

## Test Organization

```
pivox-app/
  ├── core/
  │   ├── auth/
  │   │   ├── auth_manager.cpp
  │   │   └── auth_manager_test.cpp      ← Google Test
  │   ├── grpc/
  │   │   ├── client.cpp
  │   │   └── client_test.cpp            ← Google Test
  │   └── document/
  │       ├── document_model.cpp
  │       └── document_model_test.cpp    ← Google Test
  ├── shared-ui/
  │   ├── timeline/
  │   │   ├── timeline.cpp
  │   │   └── timeline_test.cpp          ← Google Test
  ├── platform/
  │   ├── macos/
  │   │   ├── Tests/                     ← XCTest unit tests
  │   │   └── UITests/                   ← XCUITest UI tests
  │   └── windows/
  │       ├── Tests/                     ← MSTest unit tests
  │       └── UITests/                   ← WinAppDriver UI tests
  └── e2e/
      ├── macos/                         ← XCUITest E2E
      ├── windows/                       ← WinAppDriver E2E
      └── engine/                        ← gRPC test client

pivox-engine/
  ├── src/
  │   ├── compositor/
  │   │   ├── mod.rs
  │   │   └── tests.rs                   ← cargo test (inline)
  │   └── ...
  └── tests/                             ← cargo test (integration)

pivox-server/
  ├── internal/
  │   ├── playout/
  │   │   ├── controller.go
  │   │   └── controller_test.go         ← go test
  │   └── ...
  └── e2e/                               ← go test E2E
```

## CI Integration

Every PR runs:

| Stage | What | Framework | Platform |
|---|---|---|---|
| 1 | Shared C++ unit tests | Google Test | macOS + Windows |
| 2 | Engine unit + integration tests | cargo test | macOS + Linux |
| 3 | Go unit tests | go test | macOS + Linux |
| 4 | macOS native unit tests | XCTest | macOS |
| 5 | Windows native unit tests | MSTest | Windows |
| 6 | macOS UI tests | XCUITest | macOS |
| 7 | Windows UI tests | WinAppDriver | Windows |
| 8 | Engine E2E | gRPC harness | Linux (GPU runner) |
| 9 | API E2E | go test | Linux |

Stages 1-5 run in parallel. UI and E2E tests run after unit tests pass.
