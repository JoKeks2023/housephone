import Foundation
#if canImport(Darwin)
import Darwin
#endif

/// A FRITZ!Box confirmed by its TR-064 device description.
public struct FritzBoxDevice: Equatable, Sendable {
    /// IPv4 address the box answered on; used as registrar and TR-064 host.
    public var host: String
    /// E.g. "FRITZ!Box 6591 Cable".
    public var modelName: String
    /// E.g. "8.25", from `systemVersion/Display` ("161.08.25").
    public var osVersion: String?
    /// The box offers IP phones over TR-064 (`X_VoIP`).
    public var supportsIPPhones: Bool

    public init(host: String, modelName: String, osVersion: String?, supportsIPPhones: Bool) {
        self.host = host
        self.modelName = modelName
        self.osVersion = osVersion
        self.supportsIPPhones = supportsIPPhones
    }
}

/// Finds the FRITZ!Box without any entitlement: resolve `fritz.box`, then
/// try the Wi-Fi's gateways and the factory address, and confirm each with
/// `http://<ip>:49000/tr64desc.xml` (no login needed). No SSDP: multicast
/// needs an entitlement this app does not have.
public struct FritzBoxDiscovery: Sendable {
    public typealias Fetch = @Sendable (URL) async throws -> Data
    public typealias Resolve = @Sendable (String) async -> [String]

    public static let factoryAddress = "192.168.178.1"
    static let descriptionPort = 49000

    private let fetch: Fetch
    private let resolve: Resolve

    public init(fetch: Fetch? = nil, resolve: Resolve? = nil) {
        self.fetch = fetch ?? Self.fetchWithURLSession
        self.resolve = resolve ?? Self.resolveIPv4
    }

    /// The first candidate that is a FRITZ!Box. `gateways`: IPv4 gateways of
    /// the current Wi-Fi (`NWPath.gateways`).
    public func find(gateways: [String]) async -> FritzBoxDevice? {
        var candidates = await resolve("fritz.box")
        candidates += gateways
        candidates.append(Self.factoryAddress)
        var tried = Set<String>()
        for host in candidates where Self.isPrivateIPv4(host) && tried.insert(host).inserted {
            if let device = await probe(host) { return device }
        }
        return nil
    }

    /// Checks one host. Only private IPv4 addresses are asked, so a
    /// manipulated DNS answer for `fritz.box` cannot send us elsewhere.
    public func probe(_ host: String) async -> FritzBoxDevice? {
        guard Self.isPrivateIPv4(host), let url = URL(string: "http://\(host):\(Self.descriptionPort)/tr64desc.xml") else { return nil }
        guard let data = try? await fetch(url) else { return nil }
        return TR064XML.deviceDescription(data, host: host)
    }

    static func isPrivateIPv4(_ text: String) -> Bool {
        let parts = text.split(separator: ".", omittingEmptySubsequences: false).compactMap { UInt8($0) }
        guard parts.count == 4, text.split(separator: ".").count == 4 else { return false }
        switch (parts[0], parts[1]) {
        case (10, _), (192, 168): return true
        case (172, 16...31): return true
        default: return false
        }
    }

    static let fetchWithURLSession: Fetch = { url in
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 3
        configuration.timeoutIntervalForResource = 5
        configuration.allowsCellularAccess = false
        let session = URLSession(configuration: configuration)
        defer { session.finishTasksAndInvalidate() }
        let (data, response) = try await session.data(from: url)
        guard (response as? HTTPURLResponse)?.statusCode == 200, data.count <= 1 << 20 else { throw URLError(.badServerResponse) }
        return data
    }

    static let resolveIPv4: Resolve = { name in
        await Task.detached {
            var hints = addrinfo()
            hints.ai_family = AF_INET
            hints.ai_socktype = SOCK_STREAM
            var result: UnsafeMutablePointer<addrinfo>?
            guard getaddrinfo(name, nil, &hints, &result) == 0, let first = result else { return [] }
            defer { freeaddrinfo(first) }
            var addresses: [String] = []
            var cursor: UnsafeMutablePointer<addrinfo>? = first
            while let info = cursor {
                if let address = info.pointee.ai_addr, info.pointee.ai_family == AF_INET {
                    var buffer = [CChar](repeating: 0, count: Int(INET_ADDRSTRLEN))
                    address.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { pointer in
                        var addr = pointer.pointee.sin_addr
                        _ = inet_ntop(AF_INET, &addr, &buffer, socklen_t(INET_ADDRSTRLEN))
                    }
                    addresses.append(String(decoding: buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self))
                }
                cursor = info.pointee.ai_next
            }
            return addresses
        }.value
    }
}

extension TR064XML {
    /// `tr64desc.xml`: a FRITZ!Box if the manufacturer is FRITZ!/AVM and the
    /// model name says FRITZ!Box.
    public static func deviceDescription(_ data: Data, host: String) -> FritzBoxDevice? {
        guard let tree = XMLTree.parse(data), let device = tree.first(named: "device") else { return nil }
        func text(_ node: XMLTree?, _ name: String) -> String { node?.child(name)?.text.trimmingCharacters(in: .whitespacesAndNewlines) ?? "" }
        let manufacturer = text(device, "manufacturer")
        let modelName = text(device, "modelName")
        guard manufacturer.contains("FRITZ!") || manufacturer.contains("AVM"), modelName.hasPrefix("FRITZ!") else { return nil }
        let services = tree.all(named: "serviceType").map { $0.text.trimmingCharacters(in: .whitespacesAndNewlines) }
        return FritzBoxDevice(
            host: host,
            modelName: displayModel(modelName),
            osVersion: osVersion(text(tree.first(named: "systemVersion"), "Display")),
            supportsIPPhones: services.contains(TR064Client.voipService)
        )
    }

    /// "FRITZ!Box 6591 Cable (Vodafone)" → "FRITZ!Box 6591 Cable": the
    /// provider in brackets is noise in a headline.
    static func displayModel(_ name: String) -> String {
        guard name.hasSuffix(")"), let open = name.lastIndex(of: "(") else { return name }
        let trimmed = name[..<open].trimmingCharacters(in: .whitespaces)
        return trimmed.isEmpty ? name : trimmed
    }

    /// "161.08.25" → "8.25" (hardware.major.minor).
    static func osVersion(_ display: String) -> String? {
        let parts = display.split(separator: ".")
        guard parts.count == 3, let major = Int(parts[1]), let minor = Int(parts[2]) else { return nil }
        return String(format: "%d.%02d", major, minor)
    }
}
