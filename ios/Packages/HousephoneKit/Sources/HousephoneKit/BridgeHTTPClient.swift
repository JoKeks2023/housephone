import Foundation

public enum BridgeHTTPError: Error, Equatable, Sendable {
    case invalidBridgeURL
    case unexpectedStatus(Int)
    case invalidResponse
}

/// The HTTPS endpoints of signaling v1.1. The Apple Watch uses them because
/// watchOS only allows WebSocket during a CallKit call; plain HTTPS works
/// any time.
///
/// Errors: `SignalingClientError.unauthorized` for 401,
/// `SignalingClientError.bridge` when the bridge sent an `error` payload.
public struct BridgeHTTPClient: Sendable {
    private let session: URLSession

    public init(session: URLSession = .shared) {
        self.session = session
    }

    /// `wss://host/prefix/v1/ws` → `https://host/prefix/v1/<name>`.
    public static func endpoint(_ name: String, for bridgeURL: URL) -> URL? {
        guard var components = URLComponents(url: bridgeURL, resolvingAgainstBaseURL: false) else { return nil }
        switch components.scheme?.lowercased() {
        case "wss", "https": components.scheme = "https"
        case "ws", "http": components.scheme = "http"
        default: return nil
        }
        guard components.host?.isEmpty == false else { return nil }

        var base = components.path
        if base.hasSuffix("/") { base.removeLast() }
        if base.hasSuffix("/v1/ws") {
            base.removeLast("/v1/ws".count)
        } else if base.hasSuffix("/v1") {
            base.removeLast("/v1".count)
        }
        components.path = base + "/v1/" + name
        components.query = nil
        components.fragment = nil
        return components.url
    }

    /// `POST /v1/pair`.
    public func pair(link: PairingLink, deviceName: String, platform: DevicePlatform, model: String?) async throws -> BridgeCredentials {
        guard let url = Self.endpoint("pair", for: link.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        let request = PairRequest(code: link.code, deviceName: deviceName, platform: platform, model: model)
        let data = try await perform(method: "POST", url: url, body: try SignalingCoding.makeEncoder().encode(request), authorization: nil)
        guard let result = try? SignalingCoding.makeDecoder().decode(PairingResult.self, from: data) else {
            throw BridgeHTTPError.invalidResponse
        }
        return BridgeCredentials(bridgeURL: link.bridgeURL, pairing: result)
    }

    /// `PUT /v1/device`: push token, capabilities, topic.
    public func updateDevice(_ update: DeviceUpdate, credentials: BridgeCredentials) async throws {
        guard let url = Self.endpoint("device", for: credentials.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        _ = try await perform(method: "PUT", url: url, body: try SignalingCoding.makeEncoder().encode(update), authorization: credentials.authorizationHeader)
    }

    /// `DELETE /v1/device`: the device unpairs itself.
    public func deleteDevice(credentials: BridgeCredentials) async throws {
        guard let url = Self.endpoint("device", for: credentials.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        _ = try await perform(method: "DELETE", url: url, body: nil, authorization: credentials.authorizationHeader)
    }

    private func perform(method: String, url: URL, body: Data?, authorization: String?) async throws -> Data {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 15
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if let authorization {
            request.setValue(authorization, forHTTPHeaderField: "Authorization")
        }

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw BridgeHTTPError.invalidResponse }
        switch http.statusCode {
        case 200..<300:
            return data
        case 401:
            throw SignalingClientError.unauthorized
        default:
            if let payload = try? SignalingCoding.makeDecoder().decode(SignalingErrorPayload.self, from: data) {
                throw SignalingClientError.bridge(payload)
            }
            throw BridgeHTTPError.unexpectedStatus(http.statusCode)
        }
    }
}
