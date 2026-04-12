import Foundation

/// HTTP client for the delegated auth backend endpoints.
///
/// The macOS app is the receiving side of the delegated auth flow — it hands
/// a Firebase ID token back to the backend after the user signs in. It never
/// creates or polls sessions itself (that's the plugin's job). Only the
/// `completeDelegatedAuthSession` endpoint is implemented here.
///
/// The base URL comes from `PIVOX_BACKEND_URL`, falling back to the public
/// ngrok tunnel used by the rest of the native clients.
enum DelegatedAuthClient {
  /// Backend base URL. Overridable via `PIVOX_BACKEND_URL` for local dev /
  /// staging. Kept as a computed property so tests can reset the environment
  /// variable between runs.
  static var backendURL: String {
    ProcessInfo.processInfo.environment["PIVOX_BACKEND_URL"] ?? "https://pivox.ngrok.app"
  }

  /// URLSession used for every request. Injectable for tests via
  /// `URLProtocol` stubs attached to a custom configuration.
  static var urlSession: URLSession = .shared

  enum ClientError: Error, Equatable {
    case malformedBackendURL(String)
    case httpError(status: Int)
    case transport(String)
  }

  /// POST the session code to the backend with the app user's Firebase ID
  /// token. Returns true on 204 No Content.
  ///
  /// - Parameters:
  ///   - code: Session code from the `pivox://auth/delegate/signin?session=…` deep link.
  ///   - idToken: Firebase ID token for the user who just authenticated.
  /// - Returns: True iff the backend accepted the token and stored a custom token for the plugin.
  static func completeSession(code: String, idToken: String) async throws -> Bool {
    guard let url = URL(string: "\(backendURL)/internal/v1/auth:completeDelegatedAuthSession")
    else {
      throw ClientError.malformedBackendURL(backendURL)
    }

    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    request.setValue("Bearer \(idToken)", forHTTPHeaderField: "Authorization")
    request.httpBody = try JSONSerialization.data(withJSONObject: ["code": code], options: [])

    do {
      let (_, response) = try await urlSession.data(for: request)
      guard let http = response as? HTTPURLResponse else {
        throw ClientError.transport("non-HTTP response")
      }
      if http.statusCode == 204 {
        return true
      }
      // 404 = session expired/unknown; any other non-2xx is a bug.
      if (200..<300).contains(http.statusCode) {
        return true
      }
      throw ClientError.httpError(status: http.statusCode)
    } catch let error as ClientError {
      throw error
    } catch {
      throw ClientError.transport(error.localizedDescription)
    }
  }
}
