import Foundation
import Network
import Observation
import os

/// A bridge announcing itself in the home network (`_housephone._tcp`).
struct DiscoveredBridge: Identifiable, Equatable, Sendable {
    /// The Bonjour instance name, the bridge name ("Zuhause").
    let name: String
    /// The first characters of the bridge fingerprint (TXT `fp`); tells a
    /// paired bridge from another one. Not a security check: the SAS is.
    let fingerprintPrefix: String?
    let endpoint: NWEndpoint

    var id: String { name + "|" + (fingerprintPrefix ?? "") }

    static func == (lhs: Self, rhs: Self) -> Bool { lhs.id == rhs.id }
}

/// Browses for bridges while the onboarding screen is visible and resolves
/// one to the URL of its private listener (ADR-0007).
@MainActor
@Observable
final class BridgeBrowser {
    enum State: Equatable {
        case idle
        case searching
        /// Local network access was denied (Settings → Housephone).
        case denied
        case failed
    }

    private(set) var bridges: [DiscoveredBridge] = []
    private(set) var state: State = .idle

    @ObservationIgnored private var browser: NWBrowser?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "bonjour")

    static let serviceType = "_housephone._tcp"
    /// kDNSServiceErr_PolicyDenied: no local network permission.
    nonisolated static let policyDenied: Int32 = -65570

    func start() {
        guard browser == nil else { return }
        let parameters = NWParameters()
        parameters.includePeerToPeer = false
        let browser = NWBrowser(for: .bonjourWithTXTRecord(type: Self.serviceType, domain: nil), using: parameters)
        browser.stateUpdateHandler = { [weak self] newState in
            Task { @MainActor in self?.update(newState) }
        }
        browser.browseResultsChangedHandler = { [weak self] results, _ in
            let found = results.compactMap(Self.bridge(from:)).sorted { $0.name < $1.name }
            Task { @MainActor in self?.bridges = found }
        }
        browser.start(queue: .main)
        self.browser = browser
        state = .searching
    }

    func stop() {
        browser?.cancel()
        browser = nil
        bridges = []
        state = .idle
    }

    private func update(_ newState: NWBrowser.State) {
        switch newState {
        case .ready, .setup:
            state = .searching
        case .waiting(let error), .failed(let error):
            // Without local network permission the browser waits with a
            // DNS-SD "policy denied" error.
            if case .dns(let code) = error, code == Self.policyDenied {
                state = .denied
            } else {
                logger.info("Bonjour browse problem: \(error.localizedDescription, privacy: .public)")
                state = .failed
            }
        case .cancelled:
            state = .idle
        @unknown default:
            break
        }
    }

    nonisolated private static func bridge(from result: NWBrowser.Result) -> DiscoveredBridge? {
        guard case .service(let name, _, _, _) = result.endpoint else { return nil }
        var prefix: String?
        if case .bonjour(let txt) = result.metadata, case .string(let fp) = txt.getEntry(for: "fp") {
            prefix = fp
        }
        return DiscoveredBridge(name: name, fingerprintPrefix: prefix, endpoint: result.endpoint)
    }

    /// Resolves a bridge to `ws://<IPv4>:<port>/v1/ws` by connecting to it
    /// once (the bridge announces IPv4 only).
    nonisolated static func resolve(_ bridge: DiscoveredBridge, timeout: TimeInterval = 8) async throws -> URL {
        let parameters = NWParameters.tcp
        if let ip = parameters.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options {
            ip.version = .v4
        }
        let connection = NWConnection(to: bridge.endpoint, using: parameters)
        let queue = DispatchQueue(label: "com.jorisconrad.housephone.bonjour-resolve")
        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<URL, any Error>) in
            let once = OnceFlag()
            connection.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    guard once.claim() else { return }
                    if let url = connection.currentPath?.remoteEndpoint.flatMap(webSocketURL(for:)) {
                        continuation.resume(returning: url)
                    } else {
                        continuation.resume(throwing: URLError(.cannotFindHost))
                    }
                    connection.cancel()
                case .failed(let error):
                    guard once.claim() else { return }
                    continuation.resume(throwing: error)
                    connection.cancel()
                default:
                    break
                }
            }
            queue.asyncAfter(deadline: .now() + timeout) {
                guard once.claim() else { return }
                continuation.resume(throwing: URLError(.timedOut))
                connection.cancel()
            }
            connection.start(queue: queue)
        }
    }

    nonisolated static func webSocketURL(for endpoint: NWEndpoint) -> URL? {
        guard case .hostPort(let host, let port) = endpoint else { return nil }
        let hostText: String
        switch host {
        case .ipv4(let address):
            hostText = "\(address)"
        case .ipv6(let address):
            // Without the zone (fe80::1%en0): URLs can't carry it reliably.
            hostText = "[" + "\(address)".split(separator: "%").first.map(String.init)! + "]"
        case .name(let name, _):
            hostText = name
        @unknown default:
            return nil
        }
        return URL(string: "ws://\(hostText):\(port.rawValue)/v1/ws")
    }
}

/// Resumes a continuation once from a callback that may fire repeatedly.
private final class OnceFlag: @unchecked Sendable {
    private let lock = NSLock()
    private var claimed = false

    func claim() -> Bool {
        lock.withLock {
            defer { claimed = true }
            return !claimed
        }
    }
}
