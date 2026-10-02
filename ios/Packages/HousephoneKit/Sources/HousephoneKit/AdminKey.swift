import CryptoKit
import Foundation
import LocalAuthentication
import Security

/// The admin key (ADR-0009): a second P-256 key next to the device key,
/// only on admin iPhones. Unlike the device key it signs only after Face ID
/// (`.biometryCurrentSet`), so an unlocked phone alone is not enough to
/// manage the bridge. Enrolling a new face or fingerprint invalidates it;
/// an admin must then promote the device again.
public protocol AdminKeyStore: Sendable {
    /// Creates a new key under `tag`, replacing an existing one.
    func makeKey(tag: String, reason: String) throws -> any DeviceSigningKey
    /// The key under `tag`; signing asks for Face ID with `reason`.
    func key(tag: String, reason: String) throws -> (any DeviceSigningKey)?
    func deleteKey(tag: String) throws
}

public enum AdminKeyError: Error, Equatable, Sendable {
    /// No Secure Enclave or no Face ID / Touch ID set up.
    case biometryUnavailable
    /// The user cancelled the Face ID prompt.
    case cancelled
    /// The key no longer works, e.g. Face ID was set up anew.
    case invalidated
}

#if !os(watchOS)
/// Admin keys in the Secure Enclave, guarded by the current biometry. Not
/// on the watch: it is never admin.
public struct KeychainAdminKeyStore: AdminKeyStore {
    public let service: String

    public init(service: String = "com.jorisconrad.housephone.adminkey") {
        self.service = service
    }

    public func makeKey(tag: String, reason: String) throws -> any DeviceSigningKey {
        guard SecureEnclave.isAvailable, LAContext().canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil) else {
            throw AdminKeyError.biometryUnavailable
        }
        var error: Unmanaged<CFError>?
        guard let access = SecAccessControlCreateWithFlags(
            kCFAllocatorDefault,
            // Only while a passcode is set, never in backups or on another
            // device.
            kSecAttrAccessibleWhenPasscodeSetThisDeviceOnly,
            [.privateKeyUsage, .biometryCurrentSet],
            &error
        ) else {
            throw error.map { $0.takeRetainedValue() as any Error } ?? KeychainError(status: errSecParam)
        }
        let context = Self.context(reason: reason)
        let privateKey = try SecureEnclave.P256.Signing.PrivateKey(compactRepresentable: false, accessControl: access, authenticationContext: context)
        try save(privateKey.dataRepresentation, tag: tag)
        return BiometricAdminKey(privateKey: privateKey)
    }

    public func key(tag: String, reason: String) throws -> (any DeviceSigningKey)? {
        var query = baseQuery(tag: tag)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data else { return nil }
            do {
                let privateKey = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: data, authenticationContext: Self.context(reason: reason))
                return BiometricAdminKey(privateKey: privateKey)
            } catch {
                throw AdminKeyError.invalidated
            }
        case errSecItemNotFound:
            return nil
        default:
            throw KeychainError(status: status)
        }
    }

    public func deleteKey(tag: String) throws {
        let status = SecItemDelete(baseQuery(tag: tag) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw KeychainError(status: status) }
    }

    private static func context(reason: String) -> LAContext {
        let context = LAContext()
        context.localizedReason = reason
        // Every admin action asks again; no reuse of an earlier unlock.
        context.touchIDAuthenticationAllowableReuseDuration = 0
        return context
    }

    private func baseQuery(tag: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: tag,
        ]
    }

    private func save(_ data: Data, tag: String) throws {
        SecItemDelete(baseQuery(tag: tag) as CFDictionary)
        var attributes = baseQuery(tag: tag)
        attributes[kSecValueData as String] = data
        attributes[kSecAttrAccessible as String] = kSecAttrAccessibleWhenPasscodeSetThisDeviceOnly
        let status = SecItemAdd(attributes as CFDictionary, nil)
        guard status == errSecSuccess else { throw KeychainError(status: status) }
    }
}

/// A Secure Enclave key whose signing shows the Face ID prompt.
struct BiometricAdminKey: DeviceSigningKey, @unchecked Sendable {
    // An immutable value; the LAContext inside is only used by signing.
    let privateKey: SecureEnclave.P256.Signing.PrivateKey

    var publicKeyX963: Data { privateKey.publicKey.x963Representation }

    func signature(for message: Data) throws -> Data {
        do {
            return try privateKey.signature(for: message).rawRepresentation
        } catch let error as LAError where error.code == .userCancel || error.code == .appCancel || error.code == .systemCancel {
            throw AdminKeyError.cancelled
        } catch {
            throw AdminKeyError.invalidated
        }
    }
}

#endif

/// Admin keys in memory, for tests.
public final class InMemoryAdminKeyStore: AdminKeyStore, @unchecked Sendable {
    private let lock = NSLock()
    private var keys: [String: SoftwareDeviceKey] = [:]

    public init() {}

    public func makeKey(tag: String, reason: String) throws -> any DeviceSigningKey {
        let key = SoftwareDeviceKey()
        lock.withLock { keys[tag] = key }
        return key
    }

    public func key(tag: String, reason: String) throws -> (any DeviceSigningKey)? {
        lock.withLock { keys[tag] }
    }

    public func deleteKey(tag: String) throws {
        _ = lock.withLock { keys.removeValue(forKey: tag) }
    }
}
