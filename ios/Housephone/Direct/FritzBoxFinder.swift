import Foundation
import HousephoneKit
import Network

/// Looks for the FRITZ!Box on the current Wi-Fi (#30): `fritz.box`, the
/// Wi-Fi's gateways, then the factory address — see `FritzBoxDiscovery`.
enum FritzBoxFinder {
    static func find() async -> FritzBoxDevice? {
        await FritzBoxDiscovery().find(gateways: await wifiGateways())
    }

    /// The IPv4 gateways of the Wi-Fi path, or none after a short wait.
    static func wifiGateways() async -> [String] {
        let monitor = NWPathMonitor(requiredInterfaceType: .wifi)
        let queue = DispatchQueue(label: "com.jorisconrad.housephone.direct.gateways")
        let once = Once()
        return await withCheckedContinuation { continuation in
            monitor.pathUpdateHandler = { path in
                guard once.claim() else { return }
                monitor.cancel()
                continuation.resume(returning: path.gateways.compactMap { endpoint in
                    if case .hostPort(.ipv4(let address), _) = endpoint { address.debugDescription } else { nil }
                })
            }
            monitor.start(queue: queue)
            queue.asyncAfter(deadline: .now() + 2) {
                guard once.claim() else { return }
                monitor.cancel()
                continuation.resume(returning: [])
            }
        }
    }

    private final class Once: @unchecked Sendable {
        private let lock = NSLock()
        private var done = false

        func claim() -> Bool {
            lock.withLock {
                defer { done = true }
                return !done
            }
        }
    }
}
