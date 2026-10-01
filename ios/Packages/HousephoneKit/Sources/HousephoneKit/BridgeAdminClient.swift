import Foundation

// Administration from the app (ADR-0009): `/v1/admin/*` on the bridge's
// private listener only (home Wi-Fi or Tailscale). Every request is signed
// with the device key and, except the enrollment, with the admin key,
// which asks for Face ID.

/// A household profile as the admin API lists it (ADR-0008).
public struct AdminProfile: Codable, Sendable, Equatable, Identifiable, Hashable {
    public var id: String
    public var name: String
    public var numbers: [String]?
    public var registered: Bool
    public var devices: Int
    public var devicesOnline: Int

    public init(id: String, name: String, numbers: [String]? = nil, registered: Bool, devices: Int = 0, devicesOnline: Int = 0) {
        self.id = id
        self.name = name
        self.numbers = numbers
        self.registered = registered
        self.devices = devices
        self.devicesOnline = devicesOnline
    }
}

/// The bridge overview for the admin.
public struct AdminStatus: Codable, Sendable, Equatable {
    public var version: String
    public var bridgeName: String
    public var startedAt: Date?
    public var publicUrl: String?
    public var lanUrl: String?
    public var sipRegistered: Bool
    public var profiles: [AdminProfile]?
    public var publicIp: String?
    public var publicIpSource: String?
    public var apnsConfigured: Bool
    public var fritzBoxConfigured: Bool?
    public var devicesTotal: Int
    public var devicesOnline: Int
    public var activeCalls: Int
}

/// Call counters (today and since start).
public struct AdminStats: Codable, Sendable, Equatable {
    public struct Counters: Codable, Sendable, Equatable {
        public var incoming: Int
        public var answered: Int
        public var missed: Int
        public var outgoing: Int
        public var pushFailures: Int
        public var mediaFailures: Int
    }

    public var since: Date?
    public var total: Counters
    public var today: Counters
}

/// A paired device as the admin sees it.
public struct AdminDevice: Codable, Sendable, Equatable, Identifiable {
    public var id: String
    public var name: String
    public var platform: String
    public var model: String?
    public var keyFingerprint: String
    public var pairedBy: String?
    public var pairedByName: String?
    public var profile: String
    public var profileName: String
    public var admin: Bool?
    public var adminEnrolled: Bool?
    public var adminEnrollUntil: Date?
    public var createdAt: Date
    public var lastSeen: Date?
    public var online: Bool

    public init(id: String, name: String, platform: String, model: String? = nil, keyFingerprint: String = "", pairedBy: String? = nil, pairedByName: String? = nil, profile: String = BridgeProfile.defaultID, profileName: String = "", admin: Bool? = nil, adminEnrolled: Bool? = nil, adminEnrollUntil: Date? = nil, createdAt: Date, lastSeen: Date? = nil, online: Bool = false) {
        self.id = id
        self.name = name
        self.platform = platform
        self.model = model
        self.keyFingerprint = keyFingerprint
        self.pairedBy = pairedBy
        self.pairedByName = pairedByName
        self.profile = profile
        self.profileName = profileName
        self.admin = admin
        self.adminEnrolled = adminEnrolled
        self.adminEnrollUntil = adminEnrollUntil
        self.createdAt = createdAt
        self.lastSeen = lastSeen
        self.online = online
    }

    public var isAdmin: Bool { admin == true }
    public var isWatch: Bool { platform == DevicePlatform.watchos.rawValue }
    /// A watch belongs to its iPhone's profile and is moved with it.
    public var isCompanion: Bool { !(pairedBy ?? "").isEmpty }
}

/// A device in the home network waiting for approval (ADR-0007).
public struct AdminLanRequest: Codable, Sendable, Equatable, Identifiable {
    public var id: String
    public var deviceName: String
    public var platform: String
    public var model: String?
    public var ip: String
    /// The six-digit confirmation code the device shows.
    public var sas: String
    public var keyFingerprint: String
    public var expiresAt: Date

    /// `"123 456"`.
    public var groupedSAS: String {
        guard sas.count == 6 else { return sas }
        return String(sas.prefix(3)) + " " + String(sas.suffix(3))
    }
}

/// A fresh one-time pairing code (QR invite).
public struct AdminInvite: Codable, Sendable, Equatable {
    public var code: String
    public var grouped: String
    public var link: String
    public var profile: String
    public var profileName: String
    public var expiresAt: Date
}

/// Whether an invite was used.
public struct AdminInviteState: Codable, Sendable, Equatable {
    public var used: Bool
    public var expired: Bool
    public var device: AdminDevice?
}

/// What `DELETE /v1/admin/devices/{id}` removed.
public struct AdminRemoveResult: Codable, Sendable, Equatable {
    public var removed: [AdminDevice]?
    public var kept: [AdminDevice]?
}

public enum BridgeAdminError: Error, Equatable, Sendable {
    /// No home network address known: the bridge has no private listener
    /// or this pairing predates it.
    case noHomeNetworkAddress
    /// This device has no admin key; set up Face ID first.
    case notEnrolled
}

extension BridgeHTTPClient {
    /// `https://lan/v1/admin/<path>` on the private listener.
    static func adminURL(_ path: String, query: [URLQueryItem] = [], credentials: BridgeCredentials) throws -> URL {
        guard let lanURL = credentials.lanURL else { throw BridgeAdminError.noHomeNetworkAddress }
        guard let base = endpoint("admin/" + path, for: lanURL),
              var components = URLComponents(url: base, resolvingAgainstBaseURL: false)
        else { throw BridgeHTTPError.invalidBridgeURL }
        if !query.isEmpty { components.queryItems = query }
        guard let url = components.url else { throw BridgeHTTPError.invalidBridgeURL }
        return url
    }

    /// `POST /v1/admin/enroll`: registers the admin key within the window
    /// after a promotion. Signs the proof with the admin key (Face ID).
    public func enrollAdmin(key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminRole {
        let body = try SignalingCoding.makeEncoder().encode(AdminEnrollment.make(key: key, credentials: credentials))
        let data = try await send(method: "POST", url: Self.adminURL("enroll", credentials: credentials), body: body, credentials: credentials).data
        return try decode(AdminRole.self, data)
    }

    public func adminStatus(key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminStatus {
        try await admin(AdminStatus.self, "GET", "status", key: key, credentials: credentials)
    }

    public func adminStats(key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminStats {
        try await admin(AdminStats.self, "GET", "stats", key: key, credentials: credentials)
    }

    public func adminProfiles(key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> [AdminProfile] {
        try await admin([AdminProfile].self, "GET", "profiles", key: key, credentials: credentials)
    }

    public func adminDevices(key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> [AdminDevice] {
        try await admin([AdminDevice].self, "GET", "devices", key: key, credentials: credentials)
    }

    public func adminRename(_ id: String, to name: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminDevice {
        try await admin(AdminDevice.self, "PUT", "devices/\(Self.escape(id))", body: ["name": name], key: key, credentials: credentials)
    }

    public func adminRemove(_ id: String, keepCompanions: Bool, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminRemoveResult {
        let query = keepCompanions ? [URLQueryItem(name: "keepCompanions", value: "1")] : []
        return try decode(AdminRemoveResult.self, try await adminData("DELETE", "devices/\(Self.escape(id))", query: query, key: key, credentials: credentials))
    }

    public func adminMove(_ id: String, toProfile profile: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws {
        _ = try await adminData("POST", "devices/\(Self.escape(id))/move", body: ["profile": profile], key: key, credentials: credentials)
    }

    public func adminPromote(_ id: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminDevice {
        try await admin(AdminDevice.self, "POST", "devices/\(Self.escape(id))/promote", key: key, credentials: credentials)
    }

    public func adminDemote(_ id: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminDevice {
        try await admin(AdminDevice.self, "POST", "devices/\(Self.escape(id))/demote", key: key, credentials: credentials)
    }

    public func adminLanRequests(key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> [AdminLanRequest] {
        try await admin([AdminLanRequest].self, "GET", "pairings/lan", key: key, credentials: credentials)
    }

    public func adminApprove(_ id: String, profile: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminDevice {
        try await admin(AdminDevice.self, "POST", "pairings/lan/\(Self.escape(id))/approve", body: ["profile": profile], key: key, credentials: credentials)
    }

    public func adminDeny(_ id: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws {
        _ = try await adminData("POST", "pairings/lan/\(Self.escape(id))/deny", key: key, credentials: credentials)
    }

    public func adminInvite(name: String, profile: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminInvite {
        try await admin(AdminInvite.self, "POST", "pairings", body: ["name": name, "profile": profile], key: key, credentials: credentials)
    }

    public func adminInviteState(_ code: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> AdminInviteState {
        try await admin(AdminInviteState.self, "GET", "pairings/\(Self.escape(code))", key: key, credentials: credentials)
    }

    public func adminRevokeInvite(_ code: String, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws {
        _ = try await adminData("DELETE", "pairings/\(Self.escape(code))", key: key, credentials: credentials)
    }

    // MARK: - Private

    private func admin<T: Decodable>(_: T.Type, _ method: String, _ path: String, body: [String: String]? = nil, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> T {
        try decode(T.self, try await adminData(method, path, body: body, key: key, credentials: credentials))
    }

    private func adminData(_ method: String, _ path: String, query: [URLQueryItem] = [], body: [String: String]? = nil, key: any DeviceSigningKey, credentials: BridgeCredentials) async throws -> Data {
        let data = try body.map { try SignalingCoding.makeEncoder().encode($0) }
        return try await send(method: method, url: Self.adminURL(path, query: query, credentials: credentials), body: data, credentials: credentials, adminKey: key).data
    }

    private func decode<T: Decodable>(_: T.Type, _ data: Data) throws -> T {
        do {
            return try SignalingCoding.makeDecoder().decode(T.self, from: data)
        } catch {
            throw BridgeHTTPError.invalidResponse
        }
    }

    private static func escape(_ component: String) -> String {
        component.addingPercentEncoding(withAllowedCharacters: .alphanumerics.union(CharacterSet(charactersIn: "-_"))) ?? component
    }
}
