import Foundation
import Network

/// The part of the current network path the route choice cares about.
public struct NetworkPathInfo: Sendable, Equatable {
    /// Any usable network at all.
    public var isSatisfied: Bool
    /// Wi-Fi or wired Ethernet: the device may be in the home network.
    public var usesLocalNetwork: Bool
    /// A VPN interface is up (Tailscale runs as one).
    public var usesVPN: Bool

    public init(isSatisfied: Bool, usesLocalNetwork: Bool, usesVPN: Bool) {
        self.isSatisfied = isSatisfied
        self.usesLocalNetwork = usesLocalNetwork
        self.usesVPN = usesVPN
    }

    /// Without Wi-Fi, Ethernet or a VPN the private listener is out of reach.
    public var mayReachHomeNetwork: Bool { isSatisfied && (usesLocalNetwork || usesVPN) }

    public init(_ path: NWPath) {
        self.init(
            isSatisfied: path.status == .satisfied,
            usesLocalNetwork: path.usesInterfaceType(.wifi) || path.usesInterfaceType(.wiredEthernet),
            usesVPN: path.availableInterfaces.contains { $0.type == .other }
        )
    }
}

/// Watches the network path (`NWPathMonitor`) and reports changes.
public final class NetworkPathObserver: @unchecked Sendable {
    private let monitor = NWPathMonitor()
    private let queue = DispatchQueue(label: "com.jorisconrad.housephone.path")

    public init() {}

    /// Starts monitoring; `onChange` runs on a private queue for every
    /// change, starting with the current path.
    public func start(_ onChange: @escaping @Sendable (NetworkPathInfo) -> Void) {
        monitor.pathUpdateHandler = { path in onChange(NetworkPathInfo(path)) }
        monitor.start(queue: queue)
    }

    public func stop() {
        monitor.cancel()
    }
}

/// Answers whether a TCP connection to the host of a URL can be opened.
public protocol ReachabilityProbe: Sendable {
    func canConnect(to url: URL, timeout: Duration) async -> Bool
}

/// Opens (and closes) a TCP connection to the URL's host and port.
public struct TCPReachabilityProbe: ReachabilityProbe {
    public init() {}

    public func canConnect(to url: URL, timeout: Duration) async -> Bool {
        guard let host = url.host(), !host.isEmpty else { return false }
        let secure = ["wss", "https"].contains(url.scheme?.lowercased() ?? "")
        let rawPort = url.port ?? (secure ? 443 : 80)
        guard let port = NWEndpoint.Port(rawValue: UInt16(clamping: rawPort)) else { return false }
        let connection = NWConnection(host: NWEndpoint.Host(host), port: port, using: .tcp)
        let once = ResumeOnce()
        return await withCheckedContinuation { continuation in
            let finish: @Sendable (Bool) -> Void = { result in
                guard once.claim() else { return }
                connection.cancel()
                continuation.resume(returning: result)
            }
            connection.stateUpdateHandler = { state in
                switch state {
                case .ready: finish(true)
                case .failed, .cancelled: finish(false)
                // Waiting means no route right now; the timeout decides.
                default: break
                }
            }
            connection.start(queue: .global(qos: .userInitiated))
            Task {
                try? await Task.sleep(for: timeout)
                finish(false)
            }
        }
    }
}

private final class ResumeOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false

    func claim() -> Bool {
        lock.withLock {
            if done { return false }
            done = true
            return true
        }
    }
}

/// Picks the URL for the next connection: the private listener (`lanURL`)
/// when it answers within about a second, otherwise the public URL.
///
/// Both routes end at the same bridge and use the same HP2 pinning, so the
/// choice changes the path, never the trust.
public struct BridgeRouteChooser: Sendable {
    public var probe: any ReachabilityProbe
    public var probeTimeout: Duration

    public init(probe: any ReachabilityProbe = TCPReachabilityProbe(), probeTimeout: Duration = .seconds(1)) {
        self.probe = probe
        self.probeTimeout = probeTimeout
    }

    /// `path` is the current network path, `nil` if unknown (then the
    /// private listener is simply probed).
    public func url(publicURL: URL, lanURL: URL?, path: NetworkPathInfo?) async -> URL {
        guard let lanURL, lanURL != publicURL else { return publicURL }
        if let path, !path.mayReachHomeNetwork { return publicURL }
        return await probe.canConnect(to: lanURL, timeout: probeTimeout) ? lanURL : publicURL
    }
}
