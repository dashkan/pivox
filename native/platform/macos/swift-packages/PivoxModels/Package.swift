// swift-tools-version: 6.0
import PackageDescription

// PivoxModels is the Swift module that holds all generated proto types
// and the gRPC-Swift client stubs the macOS app talks to the Pivox cloud
// with. It is built via `swift build` so SwiftPM resolves grpc-swift-2's
// transitive C/Swift dependencies (NIO, BoringSSL via swift-nio-ssl)
// — the previous "scrape sources from .build/checkouts" pattern can't
// handle C deps. CMake imports the resulting static archives at link
// time. See native/CMakeLists.txt.
let package = Package(
    name: "PivoxModels",
    platforms: [.macOS(.v15)],
    products: [
        // Dynamic library so SwiftPM emits a single `.dylib` that bundles
        // PivoxModels' code AND links in all transitive dependencies
        // (SwiftProtobuf, grpc-swift-2, NIO, BoringSSL). CMake links the
        // dylib and the app target embeds it in Contents/Frameworks/ at
        // build time. Static would require each transitive dep as its
        // own .a, which SwiftPM doesn't expose cleanly.
        .library(name: "PivoxModels", type: .dynamic, targets: ["PivoxModels"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.28.0"),
        .package(url: "https://github.com/grpc/grpc-swift-2.git", from: "2.0.0"),
        .package(url: "https://github.com/grpc/grpc-swift-protobuf.git", from: "2.0.0"),
        .package(url: "https://github.com/grpc/grpc-swift-nio-transport.git", from: "2.0.0"),
        .package(url: "https://github.com/grpc/grpc-swift-extras.git", from: "2.0.0"),
    ],
    targets: [
        .target(
            name: "PivoxModels",
            dependencies: [
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
                .product(name: "GRPCCore", package: "grpc-swift-2"),
                .product(name: "GRPCProtobuf", package: "grpc-swift-protobuf"),
                .product(name: "GRPCNIOTransportHTTP2", package: "grpc-swift-nio-transport"),
            ],
            path: "Sources/PivoxModels"
        ),
    ]
)
