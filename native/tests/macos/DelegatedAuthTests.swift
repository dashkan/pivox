import XCTest

@testable import Pivox

/// Unit tests for delegated auth (AUTHN-07) on macOS.
///
/// Covers the pure-logic pieces — deep-link parsing, backend URL
/// resolution, HTTP request shape, and the signout/profile dispatch.
/// The full coordinator → Firebase → backend pipeline is covered by
/// integration tests; here we avoid touching Firebase entirely.
class DelegatedAuthTests: XCTestCase {

  // MARK: - DelegatedAuthDeepLink.parse

  func testParseSigninHappyPath() {
    let url = URL(string: "pivox://auth/delegate/signin?session=abc-123")!
    let link = DelegatedAuthDeepLink.parse(url)
    XCTAssertEqual(link?.action, .signin)
    XCTAssertEqual(link?.sessionCode, "abc-123")
  }

  func testParseSigninWithUUIDCode() {
    let url = URL(
      string: "pivox://auth/delegate/signin?session=550e8400-e29b-41d4-a716-446655440000")!
    let link = DelegatedAuthDeepLink.parse(url)
    XCTAssertEqual(link?.sessionCode, "550e8400-e29b-41d4-a716-446655440000")
  }

  func testParseProfile() {
    let url = URL(string: "pivox://auth/delegate/profile")!
    let link = DelegatedAuthDeepLink.parse(url)
    XCTAssertEqual(link?.action, .profile)
    XCTAssertNil(link?.sessionCode)
  }

  func testParseSignout() {
    let url = URL(string: "pivox://auth/delegate/signout")!
    let link = DelegatedAuthDeepLink.parse(url)
    XCTAssertEqual(link?.action, .signout)
  }

  func testParseRejectsWrongScheme() {
    XCTAssertNil(
      DelegatedAuthDeepLink.parse(URL(string: "https://auth/delegate/signin?session=x")!))
  }

  func testParseRejectsWrongHost() {
    XCTAssertNil(
      DelegatedAuthDeepLink.parse(URL(string: "pivox://other/delegate/signin?session=x")!))
  }

  func testParseRejectsUnknownPath() {
    XCTAssertNil(DelegatedAuthDeepLink.parse(URL(string: "pivox://auth/delegate/nope")!))
    XCTAssertNil(DelegatedAuthDeepLink.parse(URL(string: "pivox://auth/other/signin?session=x")!))
  }

  func testParseRejectsSigninWithoutSessionCode() {
    // signin is meaningless without a session code — drop it so the caller
    // falls back to normal URL handling instead of opening a bogus window.
    XCTAssertNil(DelegatedAuthDeepLink.parse(URL(string: "pivox://auth/delegate/signin")!))
    XCTAssertNil(DelegatedAuthDeepLink.parse(URL(string: "pivox://auth/delegate/signin?session=")!))
  }

  // MARK: - DelegatedAuthClient.backendURL

  func testBackendURLDefault() {
    unsetenv("PIVOX_BACKEND_URL")
    XCTAssertEqual(DelegatedAuthClient.backendURL, "https://pivox.ngrok.app")
  }

  func testBackendURLEnvOverride() {
    setenv("PIVOX_BACKEND_URL", "http://127.0.0.1:8080", 1)
    defer { unsetenv("PIVOX_BACKEND_URL") }
    XCTAssertEqual(DelegatedAuthClient.backendURL, "http://127.0.0.1:8080")
  }

  // MARK: - DelegatedAuthClient.completeSession

  func testCompleteSessionPostsCorrectRequestOn204() async throws {
    let session = Self.makeStubSession(status: 204, responseBody: Data())
    let previous = DelegatedAuthClient.urlSession
    DelegatedAuthClient.urlSession = session
    defer { DelegatedAuthClient.urlSession = previous }

    setenv("PIVOX_BACKEND_URL", "https://example.test", 1)
    defer { unsetenv("PIVOX_BACKEND_URL") }

    let ok = try await DelegatedAuthClient.completeSession(code: "code-42", idToken: "token-xyz")
    XCTAssertTrue(ok)

    let capture = DelegatedAuthStubURLProtocol.lastCapturedRequest
    XCTAssertEqual(
      capture?.url?.absoluteString,
      "https://example.test/internal/v1/auth:completeDelegatedAuthSession")
    XCTAssertEqual(capture?.httpMethod, "POST")
    XCTAssertEqual(capture?.value(forHTTPHeaderField: "Authorization"), "Bearer token-xyz")
    XCTAssertEqual(capture?.value(forHTTPHeaderField: "Content-Type"), "application/json")

    let bodyData = DelegatedAuthStubURLProtocol.lastCapturedBody ?? Data()
    let json = try JSONSerialization.jsonObject(with: bodyData) as? [String: String]
    XCTAssertEqual(json?["code"], "code-42")
  }

  func testCompleteSessionAcceptsAny2xx() async throws {
    let session = Self.makeStubSession(status: 200, responseBody: Data())
    let previous = DelegatedAuthClient.urlSession
    DelegatedAuthClient.urlSession = session
    defer { DelegatedAuthClient.urlSession = previous }

    let ok = try await DelegatedAuthClient.completeSession(code: "c", idToken: "t")
    XCTAssertTrue(ok)
  }

  func testCompleteSessionThrowsOn404() async {
    let session = Self.makeStubSession(status: 404, responseBody: Data())
    let previous = DelegatedAuthClient.urlSession
    DelegatedAuthClient.urlSession = session
    defer { DelegatedAuthClient.urlSession = previous }

    do {
      _ = try await DelegatedAuthClient.completeSession(code: "x", idToken: "t")
      XCTFail("Expected .httpError for 404")
    } catch DelegatedAuthClient.ClientError.httpError(let status) {
      XCTAssertEqual(status, 404)
    } catch {
      XCTFail("Unexpected error: \(error)")
    }
  }

  func testCompleteSessionThrowsOn401() async {
    let session = Self.makeStubSession(status: 401, responseBody: Data())
    let previous = DelegatedAuthClient.urlSession
    DelegatedAuthClient.urlSession = session
    defer { DelegatedAuthClient.urlSession = previous }

    do {
      _ = try await DelegatedAuthClient.completeSession(code: "x", idToken: "t")
      XCTFail("Expected httpError for 401")
    } catch DelegatedAuthClient.ClientError.httpError(let status) {
      XCTAssertEqual(status, 401)
    } catch {
      XCTFail("Unexpected error: \(error)")
    }
  }

  // MARK: - handleProfile / handleSignout

  @MainActor
  func testHandleProfilePostsNotification() {
    let expectation = expectation(
      forNotification: DelegatedAuthCoordinator.openProfileNotification,
      object: nil, handler: nil)
    DelegatedAuthCoordinator.handleProfile()
    wait(for: [expectation], timeout: 1.0)
  }

  // MARK: - Helpers

  /// Synthesise a URLSession whose data(for:) is intercepted by a URLProtocol
  /// that hands back a canned status + body.
  static func makeStubSession(status: Int, responseBody: Data) -> URLSession {
    let config = URLSessionConfiguration.ephemeral
    config.protocolClasses = [DelegatedAuthStubURLProtocol.self]
    DelegatedAuthStubURLProtocol.stubStatus = status
    DelegatedAuthStubURLProtocol.stubBody = responseBody
    DelegatedAuthStubURLProtocol.lastCapturedRequest = nil
    DelegatedAuthStubURLProtocol.lastCapturedBody = nil
    return URLSession(configuration: config)
  }
}

/// URLProtocol subclass used by the stub URLSession. Captures the outgoing
/// request (URL, method, headers, body) and returns a canned response.
///
/// Not marked `final` because `URLProtocol` requires `canInit(with:)` and
/// `canonicalRequest(for:)` as `override class func` — swiftlint's
/// `static_over_final_class` rule would otherwise flag them inside a final
/// class even though Foundation's dynamic dispatch contract requires the
/// `class` form.
class DelegatedAuthStubURLProtocol: URLProtocol {
  static var stubStatus: Int = 200
  static var stubBody: Data = Data()
  static var lastCapturedRequest: URLRequest?
  static var lastCapturedBody: Data?

  override class func canInit(with request: URLRequest) -> Bool { true }
  override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

  override func startLoading() {
    // Capture request metadata + body. URLSession moves the body into a
    // stream by this point so we read it back via the stream API.
    Self.lastCapturedRequest = request
    if let stream = request.httpBodyStream {
      stream.open()
      var data = Data()
      let bufSize = 1024
      var buf = [UInt8](repeating: 0, count: bufSize)
      while stream.hasBytesAvailable {
        let read = stream.read(&buf, maxLength: bufSize)
        if read <= 0 { break }
        data.append(buf, count: read)
      }
      stream.close()
      Self.lastCapturedBody = data
    } else {
      Self.lastCapturedBody = request.httpBody
    }

    let response = HTTPURLResponse(
      url: request.url!,
      statusCode: Self.stubStatus,
      httpVersion: "HTTP/1.1",
      headerFields: ["Content-Type": "application/json"]
    )!
    client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
    client?.urlProtocol(self, didLoad: Self.stubBody)
    client?.urlProtocolDidFinishLoading(self)
  }

  override func stopLoading() {}
}
