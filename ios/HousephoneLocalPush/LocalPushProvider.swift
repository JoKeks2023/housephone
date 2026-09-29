import Foundation
import HousephoneKit
import NetworkExtension
import os
import Security

/// Local Push Connectivity for the mode without bridge (ADR-0005): while
/// the iPhone is on the home Wi-Fi (`matchSSIDs`), iOS keeps this
/// extension running. It stays registered at the FRITZ!Box and wakes the
/// app with `reportIncomingCall` when a call comes in.
///
/// Needs the `app-push-provider` entitlement, which Apple grants on
/// request; until then the target is built by CI but not embedded.
///
/// Open (see ADR-0005): handing the ringing SIP dialog over to the app,
/// which has its own user agent. Until that exists, this provider only
/// announces the call.
final class LocalPushProvider: NEAppPushProvider, @unchecked Sendable {
    private let logger = Logger(subsystem: "com.jorisconrad.housephone.localpush", category: "provider")
    private var agent: SIPUserAgent?
    private var eventTask: Task<Void, Never>?

    override func start() {
        guard let configuration = providerConfiguration,
              let registrar = configuration["registrar"] as? String,
              let username = configuration["username"] as? String,
              let password = Self.sipPassword(accessGroup: configuration["keychainGroup"] as? String)
        else {
            logger.error("Provider configuration incomplete")
            return
        }
        let account = SIPAccount(registrar: registrar, username: username, password: password)
        let agent = SIPUserAgent(account: account, transport: UDPSIPTransport(host: registrar, port: account.port))
        self.agent = agent
        eventTask = Task { [weak self] in
            for await event in agent.events {
                guard case .incoming(let call) = event else { continue }
                self?.reportIncomingCall(userInfo: [
                    "sipCallID": call.id,
                    "number": call.number,
                    "name": call.displayName ?? "",
                ])
            }
        }
        Task { await agent.start() }
    }

    override func stop(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        logger.info("Stopping: \(reason.rawValue, privacy: .public)")
        eventTask?.cancel()
        let agent = agent
        self.agent = nil
        let done = SendableCompletion(completionHandler)
        Task {
            await agent?.stop()
            done.call()
        }
    }

    /// The SIP password from the app's keychain item, shared through a
    /// keychain access group once the entitlements exist.
    private static func sipPassword(accessGroup: String?) -> String? {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: "com.jorisconrad.housephone.direct",
            kSecAttrAccount as String: "default",
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        if let accessGroup { query[kSecAttrAccessGroup as String] = accessGroup }
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let data = result as? Data,
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return nil }
        return object["sipPassword"] as? String
    }
}

private struct SendableCompletion: @unchecked Sendable {
    let call: () -> Void

    init(_ call: @escaping () -> Void) {
        self.call = call
    }
}
