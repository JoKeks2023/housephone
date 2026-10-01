import Foundation
import HousephoneKit
import Observation
import os

/// Managing the bridge from this iPhone (ADR-0009): only for admin iPhones,
/// only over the bridge's private listener (home Wi-Fi or Tailscale), and
/// every request signed with the admin key, which asks for Face ID.
///
/// Reading (status, devices, requests) shares one Face ID check for a
/// short while; every change asks again.
@MainActor
@Observable
final class AdminCenter {
    enum Phase: Equatable {
        case idle
        case working
        case failed(String)
    }

    private(set) var phase: Phase = .idle
    private(set) var status: AdminStatus?
    private(set) var stats: AdminStats?
    private(set) var devices: [AdminDevice] = []
    private(set) var profiles: [AdminProfile] = []
    private(set) var requests: [AdminLanRequest] = []
    /// A local admin key exists for the current pairing.
    private(set) var hasLocalKey = false

    @ObservationIgnored private let bridge: BridgeConnection
    @ObservationIgnored private let keys: any AdminKeyStore
    @ObservationIgnored private var readKey: (key: any DeviceSigningKey, until: Date)?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "admin")

    /// How long one Face ID check covers reading.
    static let readWindow: TimeInterval = 120

    init(bridge: BridgeConnection, keys: any AdminKeyStore = KeychainAdminKeyStore()) {
        self.bridge = bridge
        self.keys = keys
        syncLocalKey()
    }

    var role: AdminRole? { bridge.adminRole }

    /// The bridge has this iPhone as admin.
    var isAdmin: Bool { role?.admin == true }

    /// Management works only over the home network listener.
    var isReachable: Bool { bridge.isOnline && bridge.isOnHomeNetwork }

    /// The bridge knows this iPhone's Face ID key and it is still here.
    var isReady: Bool { isAdmin && role?.enrolled == true && hasLocalKey }

    /// Admin, but Face ID needs (re)setting up and the window is open.
    var canEnroll: Bool { role?.canEnroll() == true }

    var multiProfile: Bool { profiles.count > 1 }

    private var keyTag: String? {
        bridge.credentials.map { "admin-\($0.deviceId)" }
    }

    /// Keeps the local key in step with the role: a demoted iPhone forgets
    /// its admin key.
    func syncLocalKey() {
        guard let keyTag else {
            hasLocalKey = false
            return
        }
        if bridge.credentials != nil, bridge.welcome != nil, !isAdmin {
            try? keys.deleteKey(tag: keyTag)
            readKey = nil
        }
        hasLocalKey = (try? keys.key(tag: keyTag, reason: "")) != nil
    }

    // MARK: - Face ID

    /// Creates the admin key and registers it at the bridge (one Face ID
    /// check for the proof).
    func enroll() async {
        guard let credentials = bridge.credentials, let keyTag else { return }
        phase = .working
        do {
            let key = try keys.makeKey(tag: keyTag, reason: String(localized: "Verwaltung mit Face ID absichern"))
            let role = try await bridge.httpClient().enrollAdmin(key: key, credentials: credentials)
            hasLocalKey = true
            phase = .idle
            logger.info("Admin key enrolled (enrolled: \(role.enrolled))")
            await refresh()
        } catch {
            try? keys.deleteKey(tag: keyTag)
            hasLocalKey = false
            fail(error)
        }
    }

    // MARK: - Reading

    func refresh() async {
        guard let credentials = bridge.credentials else { return }
        phase = .working
        do {
            let key = try readingKey()
            let client = bridge.httpClient()
            // One after the other: the first signature shows Face ID, the
            // others reuse that check.
            status = try await client.adminStatus(key: key, credentials: credentials)
            profiles = try await client.adminProfiles(key: key, credentials: credentials)
            devices = try await client.adminDevices(key: key, credentials: credentials)
            requests = try await client.adminLanRequests(key: key, credentials: credentials)
            stats = try await client.adminStats(key: key, credentials: credentials)
            phase = .idle
        } catch {
            readKey = nil
            fail(error)
        }
    }

    /// Forgets the shared Face ID check, e.g. when the screen closes.
    func lock() {
        readKey = nil
    }

    // MARK: - Changes (each asks for Face ID)

    func rename(_ device: AdminDevice, to name: String) async {
        await change(String(localized: "„\(device.name)“ umbenennen")) { client, key, credentials in
            _ = try await client.adminRename(device.id, to: name, key: key, credentials: credentials)
        }
    }

    func remove(_ device: AdminDevice, keepCompanions: Bool) async {
        await change(String(localized: "„\(device.name)“ entfernen")) { client, key, credentials in
            _ = try await client.adminRemove(device.id, keepCompanions: keepCompanions, key: key, credentials: credentials)
        }
    }

    func move(_ device: AdminDevice, to profile: AdminProfile) async {
        await change(String(localized: "„\(device.name)“ nach „\(profile.name)“ verschieben")) { client, key, credentials in
            try await client.adminMove(device.id, toProfile: profile.id, key: key, credentials: credentials)
        }
    }

    func promote(_ device: AdminDevice) async {
        await change(String(localized: "„\(device.name)“ zum Admin machen")) { client, key, credentials in
            _ = try await client.adminPromote(device.id, key: key, credentials: credentials)
        }
    }

    func demote(_ device: AdminDevice) async {
        await change(String(localized: "„\(device.name)“ die Admin-Rechte entziehen")) { client, key, credentials in
            _ = try await client.adminDemote(device.id, key: key, credentials: credentials)
        }
    }

    func approve(_ request: AdminLanRequest, profile: AdminProfile?) async {
        await change(String(localized: "„\(request.deviceName)“ freigeben")) { client, key, credentials in
            _ = try await client.adminApprove(request.id, profile: profile?.id ?? "", key: key, credentials: credentials)
        }
    }

    func deny(_ request: AdminLanRequest) async {
        await change(String(localized: "Anfrage von „\(request.deviceName)“ ablehnen")) { client, key, credentials in
            try await client.adminDeny(request.id, key: key, credentials: credentials)
        }
    }

    /// A fresh one-time code with QR link for a new device.
    func invite(name: String, profile: AdminProfile?) async -> AdminInvite? {
        var invite: AdminInvite?
        await change(String(localized: "Neues Gerät einladen")) { client, key, credentials in
            invite = try await client.adminInvite(name: name, profile: profile?.id ?? "", key: key, credentials: credentials)
        }
        return invite
    }

    func revoke(_ invite: AdminInvite) async {
        guard let credentials = bridge.credentials, let key = try? readingKey() else { return }
        try? await bridge.httpClient().adminRevokeInvite(invite.code, key: key, credentials: credentials)
    }

    /// Whether the invite was used (polled while its QR code is shown).
    func inviteState(_ invite: AdminInvite) async -> AdminInviteState? {
        guard let credentials = bridge.credentials, let key = try? readingKey() else { return nil }
        return try? await bridge.httpClient().adminInviteState(invite.code, key: key, credentials: credentials)
    }

    // MARK: - Private

    private func readingKey() throws -> any DeviceSigningKey {
        if let readKey, readKey.until > Date() { return readKey.key }
        let key = try freshKey(reason: String(localized: "Verwaltung öffnen"))
        readKey = (key, Date().addingTimeInterval(Self.readWindow))
        return key
    }

    private func freshKey(reason: String) throws -> any DeviceSigningKey {
        guard let keyTag, let key = try keys.key(tag: keyTag, reason: reason) else {
            hasLocalKey = false
            throw BridgeAdminError.notEnrolled
        }
        return key
    }

    private func change(_ reason: String, _ operation: (BridgeHTTPClient, any DeviceSigningKey, BridgeCredentials) async throws -> Void) async {
        guard let credentials = bridge.credentials else { return }
        phase = .working
        do {
            let key = try freshKey(reason: reason)
            try await operation(bridge.httpClient(), key, credentials)
            phase = .idle
        } catch {
            fail(error)
            return
        }
        await refresh()
    }

    private func fail(_ error: any Error) {
        if let message = Self.message(for: error) {
            phase = .failed(message)
            if error as? AdminKeyError == .invalidated || error as? BridgeAdminError == .notEnrolled {
                hasLocalKey = false
            }
        } else {
            phase = .idle
        }
    }

    /// What to tell the user; `nil` for a cancelled Face ID prompt.
    static func message(for error: any Error) -> String? {
        if let error = error as? AdminKeyError {
            switch error {
            case .cancelled:
                return nil
            case .invalidated:
                return String(localized: "Face ID wurde geändert. Lass dich auf dem Server oder von einem anderen Admin erneut freischalten und richte Face ID dann neu ein.")
            case .biometryUnavailable:
                return String(localized: "Die Verwaltung braucht Face ID oder Touch ID. Richte es in den iOS-Einstellungen ein.")
            }
        }
        if let error = error as? BridgeAdminError {
            switch error {
            case .notEnrolled:
                return String(localized: "Face ID wurde geändert. Lass dich auf dem Server oder von einem anderen Admin erneut freischalten und richte Face ID dann neu ein.")
            case .noHomeNetworkAddress:
                return String(localized: "Die Verwaltung ist nur im Heim-WLAN oder über Tailscale erreichbar.")
            }
        }
        if error is URLError {
            return String(localized: "Die Verwaltung ist nur im Heim-WLAN oder über Tailscale erreichbar.")
        }
        if case SignalingClientError.bridge(let payload)? = error as? SignalingClientError {
            switch payload.code {
            case .adminRequired:
                return String(localized: "Die Bridge erkennt dieses iPhone nicht mehr als Admin.")
            case .adminEnrollClosed:
                return String(localized: "Die Zeit zum Einrichten ist abgelaufen. Lass dich erneut zum Admin machen.")
            case .homeNetworkRequired:
                return String(localized: "Die Verwaltung ist nur im Heim-WLAN oder über Tailscale erreichbar.")
            case .notAllowed:
                return String(localized: "Nur ein iPhone kann Admin sein, keine Apple Watch.")
            case .notFound:
                return String(localized: "Das Gerät oder die Anfrage gibt es nicht mehr.")
            default:
                break
            }
        }
        Logger(subsystem: "com.jorisconrad.housephone", category: "admin").error("Admin request failed: \(String(describing: error), privacy: .public)")
        return String(localized: "Das hat nicht geklappt. Versuch es noch einmal.")
    }
}
