import Foundation
import Security

/// Links that widgets, controls, complications and CarPlay use to open the
/// app. Extensions never call themselves; the app starts the call.
///
/// - `housephone://call?number=…&name=…&key=…`
/// - `housephone://keypad`
/// - `housephone://recents` and `housephone://recents?missed=1`
///
/// Pairing links (`housephone://pair`) are `PairingLink`.
public enum DeepLink: Equatable, Sendable {
    /// `key` is `DeepLinkKey` when the link comes from one of the app's own
    /// extensions. Without it (any other app or a web page) the app asks
    /// before calling, like `tel:` links do.
    case call(number: String, name: String?, key: String?)
    case keypad
    case recents(missedOnly: Bool)

    public static let scheme = "housephone"

    public init?(url: URL) {
        guard url.scheme?.lowercased() == Self.scheme,
              let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        else { return nil }
        func value(_ name: String) -> String? {
            components.queryItems?.first { $0.name == name }?.value.flatMap { $0.isEmpty ? nil : $0 }
        }
        switch url.host()?.lowercased() {
        case "call":
            guard let number = value("number").flatMap(PhoneNumber.dialable) else { return nil }
            self = .call(number: number, name: value("name"), key: value("key"))
        case "keypad":
            self = .keypad
        case "recents":
            self = .recents(missedOnly: value("missed") == "1")
        default:
            return nil
        }
    }

    public var url: URL {
        var components = URLComponents()
        components.scheme = Self.scheme
        switch self {
        case .call(let number, let name, let key):
            components.host = "call"
            components.queryItems = [URLQueryItem(name: "number", value: number)]
            if let name { components.queryItems?.append(URLQueryItem(name: "name", value: name)) }
            if let key { components.queryItems?.append(URLQueryItem(name: "key", value: key)) }
        case .keypad:
            components.host = "keypad"
        case .recents(let missedOnly):
            components.host = "recents"
            if missedOnly { components.queryItems = [URLQueryItem(name: "missed", value: "1")] }
        }
        // Only scheme, host and query items: always a valid URL.
        return components.url!
    }
}

/// A random key in the App Group that only the app and its extensions can
/// read. Call links carrying it start the call without asking.
public enum DeepLinkKey {
    static let fileName = "link-key"

    /// The stored key, or `nil` before the app created it.
    public static func load(from directory: URL) -> String? {
        guard let data = try? Data(contentsOf: directory.appending(path: fileName)),
              let key = String(data: data, encoding: .utf8), !key.isEmpty
        else { return nil }
        return key
    }

    /// For the app: the stored key, created on first use.
    public static func loadOrCreate(in directory: URL) throws -> String {
        if let key = load(from: directory) { return key }
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else {
            throw CocoaError(.fileWriteUnknown)
        }
        let key = Data(bytes).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try Data(key.utf8).write(to: directory.appending(path: fileName), options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        return key
    }

    /// Compares without returning early, so timing reveals nothing.
    public static func matches(_ candidate: String?, expected: String?) -> Bool {
        guard let candidate, let expected, !expected.isEmpty else { return false }
        let a = Array(candidate.utf8), b = Array(expected.utf8)
        guard a.count == b.count else { return false }
        return zip(a, b).reduce(0) { $0 | ($1.0 ^ $1.1) } == 0
    }
}
