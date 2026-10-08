// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "HousephoneKit",
    platforms: [.iOS(.v17), .watchOS(.v10), .macOS(.v15)],
    products: [
        .library(name: "HousephoneKit", targets: ["HousephoneKit"]),
    ],
    targets: [
        .target(name: "HousephoneKit"),
        .testTarget(name: "HousephoneKitTests", dependencies: ["HousephoneKit"]),
    ]
)
