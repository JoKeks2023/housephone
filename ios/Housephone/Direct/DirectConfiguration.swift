import Foundation
import HousephoneKit
import Security

/// Setup of the mode without bridge (ADR-0005): the IP-phone account on
/// the FRITZ!Box, the home Wi-Fi and optional TR-064 access. Stored as one
/// keychain item, passwords included — never in `UserDefaults` or logs.
struct DirectConfiguration: Codable, Equatable, Sendable {
    /// Host name or IP of the FRITZ!Box, e.g. `fritz.box` or `192.168.178.1`.
    var registrar = "fritz.box"
    /// User name of the IP phone (Telefonie → Telefoniegeräte).
    var sipUsername = ""
    var sipPassword = ""
    /// Name of the home Wi-Fi, for ringing in the background (Local Push).
    var homeSSID = ""
    var usesTR064 = false
    var tr064Username = ""
    var tr064Password = ""
    /// Set when Housephone created the IP phone itself (HPHN-25): its
    /// `X_AVM-DE_ClientId`, to find and reuse it on the next setup.
    var fritzBoxClientID: String?

    var account: SIPAccount {
        SIPAccount(registrar: registrar.trimmingCharacters(in: .whitespaces), username: sipUsername.trimmingCharacters(in: .whitespaces), password: sipPassword)
    }

    var isComplete: Bool {
        !registrar.trimmingCharacters(in: .whitespaces).isEmpty
            && !sipUsername.trimmingCharacters(in: .whitespaces).isEmpty
            && !sipPassword.isEmpty
            && (!usesTR064 || !tr064Password.isEmpty)
    }

    var tr064Client: TR064Client? {
        guard usesTR064, !tr064Password.isEmpty else { return nil }
        return TR064Client(host: account.registrar, username: tr064Username.trimmingCharacters(in: .whitespaces), password: tr064Password)
    }
}

/// Keychain item for `DirectConfiguration`, readable after the first
/// unlock (an incoming call may arrive while locked), never migrated to
/// another device.
struct DirectConfigurationStore: Sendable {
    let service = "com.jorisconrad.housephone.direct"
    let account = "default"

    private var baseQuery: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    func load() -> DirectConfiguration? {
        var query = baseQuery
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let data = result as? Data else { return nil }
        return try? JSONDecoder().decode(DirectConfiguration.self, from: data)
    }

    func save(_ configuration: DirectConfiguration) throws {
        let data = try JSONEncoder().encode(configuration)
        let attributes: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let status = SecItemUpdate(baseQuery as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            let added = SecItemAdd(baseQuery.merging(attributes) { $1 } as CFDictionary, nil)
            guard added == errSecSuccess else { throw DirectConfigurationError(status: added) }
        } else if status != errSecSuccess {
            throw DirectConfigurationError(status: status)
        }
    }

    func delete() {
        SecItemDelete(baseQuery as CFDictionary)
    }
}

struct DirectConfigurationError: Error {
    let status: OSStatus
}
