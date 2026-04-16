// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "PivoxModels",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "PivoxModels", targets: ["PivoxModels"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.28.0"),
    ],
    targets: [
        .target(
            name: "PivoxModels",
            dependencies: [
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            path: "Sources/PivoxModels"
        ),
    ]
)
