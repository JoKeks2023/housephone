import Foundation
import Security

/// What a device keeps after pairing. Lives in the keychain.
///
/// There is no secret in here (ADR-0004): the device signs with its own
/// key (`keyTag` in the `DeviceKeyStore`), and `bridgePublicKey` is the
/// bridge identity pinned at pairing against the QR code's fingerprint.
public struct BridgeCredentials: Codable, Equatable, Sendable {
    public var bridgeURL: URL
    public var deviceId: DeviceID
    public var bridgeId: String
    public var bridgeName: String
    /// The bridge's Ed25519 public key (32 bytes), pinned at pairing.
    public var bridgePublicKey: Data
    /// Tag of this device's signing key in the `DeviceKeyStore`.
    public var keyTag: String

    public init(bridgeURL: URL, deviceId: DeviceID, bridgeId: String, bridgeName: String, bridgePublicKey: Data, keyTag: String) {
        self.bridgeURL = bridgeURL
        self.deviceId = deviceId
        self.bridgeId = bridgeId
        self.bridgeName = bridgeName
        self.bridgePublicKey = bridgePublicKey
        self.keyTag = keyTag
    }

    /// `base64url(SHA-256(bridgePublicKey))`, as in the pairing link
    /// (`fp`); shown in Settings to compare with the bridge.
    public var bridgeFingerprint: String { HP2.fingerprint(of: bridgePublicKey) }
}

public protocol CredentialStore: Sendable {
    func load() throws -> BridgeCredentials?
    func save(_ credentials: BridgeCredentials) throws
    func delete() throws
}

public struct KeychainError: Error, Equatable, CustomStringConvertible {
    public let status: OSStatus

    public var description: String {
        let message = SecCopyErrorMessageString(status, nil) as String? ?? "unknown"
        return "Keychain error \(status): \(message)"
    }
}

/// Stores credentials as one generic-password item.
///
/// Uses `AfterFirstUnlockThisDeviceOnly`: a VoIP push can arrive while the
/// phone is locked, and a pairing must never move to another device via
/// backups (the device key can't anyway).
public struct KeychainCredentialStore: CredentialStore {
    public let service: String
    public let account: String

    public init(service: String = "com.jorisconrad.housephone.bridge", account: String = "default") {
        self.service = service
        self.account = account
    }

    private var baseQuery: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    public func load() throws -> BridgeCredentials? {
        var query = baseQuery
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data else { return nil }
            return try JSONDecoder().decode(BridgeCredentials.self, from: data)
        case errSecItemNotFound:
            return nil
        default:
            throw KeychainError(status: status)
        }
    }

    public func save(_ credentials: BridgeCredentials) throws {
        let data = try JSONEncoder().encode(credentials)
        let attributes: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]

        let status = SecItemUpdate(baseQuery as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            let addStatus = SecItemAdd(baseQuery.merging(attributes) { $1 } as CFDictionary, nil)
            guard addStatus == errSecSuccess else { throw KeychainError(status: addStatus) }
        } else if status != errSecSuccess {
            throw KeychainError(status: status)
        }
    }

    public func delete() throws {
        let status = SecItemDelete(baseQuery as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw KeychainError(status: status) }
    }
}

/// For tests and SwiftUI previews.
public final class InMemoryCredentialStore: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private var credentials: BridgeCredentials?

    public init(_ credentials: BridgeCredentials? = nil) {
        self.credentials = credentials
    }

    public func load() throws -> BridgeCredentials? {
        lock.withLock { credentials }
    }

    public func save(_ credentials: BridgeCredentials) throws {
        lock.withLock { self.credentials = credentials }
    }

    public func delete() throws {
        lock.withLock { credentials = nil }
    }
}
