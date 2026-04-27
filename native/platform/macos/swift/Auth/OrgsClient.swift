import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2
import GRPCProtobuf
import PivoxModels
import SwiftProtobuf

/// Pivox cloud gRPC client for the Organizations service. Pure Swift
/// (grpc-swift-2). Wraps the generated `Pivox_Api_V1_Organizations.Client`
/// and shares the same plaintext HTTP/2 + Firebase-auth-interceptor
/// pattern as `ChatClient`.
///
/// Auth: every outbound RPC carries the current Firebase user's ID
/// token via `FirebaseAuthInterceptor` (defined in ChatClient.swift,
/// file-internal but module-visible).
@MainActor
final class OrgsClient {
    private let grpc: GRPCClient<HTTP2ClientTransport.Posix>
    private let orgs: Pivox_Api_V1_Organizations.Client<HTTP2ClientTransport.Posix>
    private var runTask: Task<Void, Error>?

    init() throws {
        let (host, port) = try CloudConfig.parsedEndpoint()
        let transport = try HTTP2ClientTransport.Posix(
            target: .dns(host: host, port: port),
            transportSecurity: CloudConfig.transportSecurity
        )
        self.grpc = GRPCClient(
            transport: transport,
            interceptors: [FirebaseAuthInterceptor()]
        )
        self.orgs = Pivox_Api_V1_Organizations.Client(wrapping: grpc)
        self.runTask = Task { [grpc] in
            try await grpc.runConnections()
        }
    }

    func cancel() {
        runTask?.cancel()
        runTask = nil
        grpc.beginGracefulShutdown()
    }

    // MARK: - RPCs

    func listOrganizations() async throws -> [Pivox_Api_V1_Organization] {
        let response = try await orgs.listOrganizations(
            Pivox_Api_V1_ListOrganizationsRequest()
        )
        return response.organizations
    }

    /// Creates an organization. The server returns a synchronously-
    /// completed `google.longrunning.Operation` with the created
    /// Organization packed in `response`; this helper unpacks it.
    func createOrganization(
        displayName: String,
        organizationID: String
    ) async throws -> Pivox_Api_V1_Organization {
        var req = Pivox_Api_V1_CreateOrganizationRequest()
        var org = Pivox_Api_V1_Organization()
        org.displayName = displayName
        req.organization = org
        req.organizationID = organizationID

        let op = try await orgs.createOrganization(req)
        guard op.done else {
            throw OrgsClientError.operationNotDone
        }
        switch op.result {
        case .response(let any):
            return try Pivox_Api_V1_Organization(unpackingAny: any)
        case .error(let status):
            throw OrgsClientError.operationFailed(status.message)
        case .none:
            throw OrgsClientError.operationMissingResult
        }
    }

    // MARK: -

}

enum OrgsClientError: Error, CustomStringConvertible {
    case operationNotDone
    case operationMissingResult
    case operationFailed(String)

    var description: String {
        switch self {
        case .operationNotDone: return "Server returned an in-progress operation."
        case .operationMissingResult: return "Server returned an empty operation."
        case .operationFailed(let m): return m
        }
    }
}
