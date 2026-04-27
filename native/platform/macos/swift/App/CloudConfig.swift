import Foundation
import GRPCNIOTransportHTTP2

/// Single source of truth for the Pivox cloud gRPC endpoint and
/// transport security. Both `ChatClient` and `OrgsClient` resolve
/// through here so they can't drift.
///
/// Resolution order:
///   1. `PIVOX_GRPC_HOST` env var, e.g. "localhost:50051" (no scheme).
///      `PIVOX_GRPC_PLAINTEXT=1` disables TLS — useful for local dev
///      pointing at a plaintext server on a non-443 port.
///   2. Default: `pivox.ngrok.app:443` over TLS. Matches the public
///      tunnel used by the OAuth broker, so a single environment
///      override (or no override at all) keeps gRPC + REST + auth
///      pointing at the same backend.
///
/// Local-dev workflow: export `PIVOX_GRPC_HOST=localhost:50051` and
/// `PIVOX_GRPC_PLAINTEXT=1` in `.envrc`. Production builds inherit
/// the ngrok default.
enum CloudConfig {
  /// "host:port" string, no scheme.
  static let grpcEndpoint: String = {
    if let override = ProcessInfo.processInfo.environment["PIVOX_GRPC_HOST"],
       !override.isEmpty {
      return override
    }
    return "pivox.ngrok.app:443"
  }()

  /// Whether to negotiate TLS on the gRPC channel. ngrok terminates
  /// TLS on :443; local dev typically runs plaintext on :50051.
  static let usePlaintext: Bool = {
    ProcessInfo.processInfo.environment["PIVOX_GRPC_PLAINTEXT"] == "1"
  }()

  /// Resolved transport security for grpc-swift-2's
  /// `HTTP2ClientTransport.Posix`. Use this directly when constructing
  /// a client transport.
  static var transportSecurity: HTTP2ClientTransport.Posix.TransportSecurity {
    usePlaintext ? .plaintext : .tls
  }

  /// Parses `grpcEndpoint` into (host, port). Throws on malformed
  /// values rather than silently picking a default — a typo here
  /// would otherwise misroute every RPC.
  static func parsedEndpoint() throws -> (host: String, port: Int) {
    let parts = grpcEndpoint.split(separator: ":", maxSplits: 1).map(String.init)
    guard parts.count == 2, let port = Int(parts[1]) else {
      throw CloudConfigError.invalidEndpoint(grpcEndpoint)
    }
    return (parts[0], port)
  }
}

enum CloudConfigError: Error, CustomStringConvertible {
  case invalidEndpoint(String)

  var description: String {
    switch self {
    case .invalidEndpoint(let s): return "invalid PIVOX_GRPC_HOST: \(s)"
    }
  }
}
