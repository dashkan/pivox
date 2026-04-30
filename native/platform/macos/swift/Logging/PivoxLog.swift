import Foundation
import OSLog

/// Pivox unified logging surface for the macOS app.
///
/// Wraps Apple's `os.Logger` so:
///   - Every log line gets a categorized subsystem visible in
///     Console.app under `app.pivox.native` filtered by category.
///   - Sensitive payloads (id_tokens, NSError userInfo, request
///     bodies) go through `debugSensitive(_:)` which compiles to a
///     no-op in release builds — they never reach the unified log
///     in shipping binaries, eliminating a class of token-leak
///     incidents from production crash reports.
///   - Call sites stay terse: `PivoxLog.sso.error("…")` instead of
///     constructing OSLog/Logger instances inline.
///
/// Subsystem: `app.pivox.native` (matches the bundle identifier so
/// log streaming with `log stream --predicate 'subsystem == "app.pivox.native"'`
/// captures everything from the app — auth, chat, transcript, etc.).
/// Add a new category by appending a `Logger` to the enum below.
enum PivoxLog {
  // Subsystem matches CFBundleIdentifier so unified-log filtering
  // works without a separate predicate per category.
  private static let subsystem = "app.pivox.native"

  /// Application lifecycle, scene/window management, AppDelegate.
  static let app = Logger(subsystem: subsystem, category: "app")

  /// Sign-in / sign-out flows: email/password, Google, GitHub,
  /// delegated auth, MFA. SSO has its own category below.
  static let auth = Logger(subsystem: subsystem, category: "auth")

  /// Enterprise SSO (OIDC) flows. Separate from `auth` because
  /// SSO failure modes (broker round-trip, OAuthCredential build,
  /// Firebase id_token verification) are distinct from password /
  /// social flows and benefit from independent filtering.
  static let sso = Logger(subsystem: subsystem, category: "sso")

  /// AI chat client (gRPC channel lifecycle, ChatClient errors).
  static let chat = Logger(subsystem: subsystem, category: "chat")

  /// Transcript pagination, scroll behavior, message rendering.
  static let transcript = Logger(subsystem: subsystem, category: "transcript")
}

extension Logger {
  /// Logs a message that may contain sensitive data (raw tokens,
  /// NSError userInfo, request bodies, etc.). In DEBUG builds this
  /// routes to `.debug` with public-string interpolation so dev
  /// inspection works. In RELEASE the call compiles to a no-op —
  /// the message is never built and never reaches the unified log.
  ///
  /// Use this anywhere you'd otherwise reach for `print(...)` of
  /// debug-only diagnostic data. Concrete examples:
  ///   - Firebase NSError.userInfo (can include id_token)
  ///   - HTTP request bodies pre-redaction
  ///   - Decoded JWT payloads during dev
  ///
  /// Non-sensitive operational logging should use the standard
  /// `info` / `notice` / `error` methods directly; those DO ship
  /// to release builds.
  ///
  /// `@autoclosure` defers message construction so the no-op path
  /// in release also skips any string interpolation cost.
  ///
  /// Note: we don't use the `\(value, privacy: .public)` directive
  /// here because the OSLogMessage interpolation extensions are
  /// only callable in contexts the compiler recognizes as
  /// OSLogMessage literals. From this generic wrapper we'd need
  /// to expose an OSLogMessage-bearing API. The `#if DEBUG` gate
  /// is the actual protection — release builds compile this out
  /// entirely, so unified-log redaction wouldn't matter anyway.
  @inlinable
  func debugSensitive(_ message: @autoclosure () -> String) {
    #if DEBUG
      let s = message()
      self.debug("\(s)")
    #endif
  }
}
