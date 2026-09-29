// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "HousephoneKit",
    platforms: [.iOS(.v26), .watchOS(.v26), .macOS(.v15)],
    products: [
        .library(name: "HousephoneKit", targets: ["HousephoneKit"]),
    ],
    targets: [
        .target(name: "HousephoneKit"),
        .testTarget(name: "HousephoneKitTests", dependencies: ["HousephoneKit"]),
    ]
)
