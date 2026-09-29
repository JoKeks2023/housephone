import Foundation

public enum BridgeHTTPError: Error, Equatable, Sendable {
    case invalidBridgeURL
    case unexpectedStatus(Int)
    case invalidResponse
}

/// A call as `GET /v1/calls/{callId}` reports it for this device.
public struct BridgeCallStatus: Sendable, Equatable {
    public enum State: Sendable, Equatable {
        /// Incoming: still ringing. Outgoing: dialing or the remote rings.
        /// Unknown states from newer bridges count as still active.
        case ringing
        /// This device holds the call.
        case connected
        /// Over for this device, e.g. `.answeredElsewhere` or `.remoteCancelled`.
        case ended(CallEndReason, sipCode: Int?)
    }

    public var callId: CallID
    public var state: State

    public init(callId: CallID, state: State) {
        self.callId = callId
        self.state = state
    }
}

/// Wire format of `GET /v1/calls/{callId}`.
struct CallStatusPayload: Decodable {
    var callId: CallID
    var state: String
    var reason: CallEndReason?
    var sipCode: Int?

    var status: BridgeCallStatus {
        switch state {
        case "connected":
            BridgeCallStatus(callId: callId, state: .connected)
        case "ended":
            BridgeCallStatus(callId: callId, state: .ended(reason ?? .failed, sipCode: sipCode))
        default:
            BridgeCallStatus(callId: callId, state: .ringing)
        }
    }
}

/// The HTTPS endpoints of signaling v1.1 and v1.2. The Apple Watch uses
/// them because watchOS only allows WebSocket during a CallKit call; plain
/// HTTPS works any time. Phonebook and call list are HTTPS-only on both.
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

    /// `GET /v1/calls/{callId}`: the call from this device's point of view.
    /// A ringing watch polls this while it may not open a WebSocket. A call
    /// the bridge no longer knows (404 `call_not_found`) is reported as
    /// ended with `.notFound`.
    public func callStatus(_ callId: CallID, credentials: BridgeCredentials) async throws -> BridgeCallStatus {
        guard let url = Self.endpoint("calls/\(callId)", for: credentials.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        let data: Data
        do {
            data = try await perform(method: "GET", url: url, body: nil, authorization: credentials.authorizationHeader)
        } catch SignalingClientError.bridge(let error) where error.code == .callNotFound {
            return BridgeCallStatus(callId: callId, state: .ended(.notFound, sipCode: nil))
        }
        guard let payload = try? SignalingCoding.makeDecoder().decode(CallStatusPayload.self, from: data) else {
            throw BridgeHTTPError.invalidResponse
        }
        return payload.status
    }

    /// `GET /v1/phonebook` (v1.2): all FRITZ!Box phonebooks merged. With
    /// the ETag of the cached copy the bridge answers `.notModified` when
    /// nothing changed. Errors: `SignalingClientError.bridge` with
    /// `fritzbox_unavailable` when the bridge has no working FRITZ!Box access.
    public func phonebook(ifNoneMatch etag: String?, credentials: BridgeCredentials) async throws -> FritzBoxFetchResult<FritzBoxPhonebook> {
        guard let url = Self.endpoint("phonebook", for: credentials.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        let headers = etag.map { ["If-None-Match": $0] } ?? [:]
        let (data, response) = try await send(method: "GET", url: url, body: nil, authorization: credentials.authorizationHeader, headers: headers)
        if response.statusCode == 304 { return .notModified }
        guard let phonebook = try? SignalingCoding.makeDecoder().decode(FritzBoxPhonebook.self, from: data) else {
            throw BridgeHTTPError.invalidResponse
        }
        return .updated(phonebook, etag: response.value(forHTTPHeaderField: "ETag"))
    }

    /// `GET /v1/history` (v1.2): the line's call list, newest first.
    public func history(limit: Int = 100, credentials: BridgeCredentials) async throws -> FritzBoxCallList {
        guard let base = Self.endpoint("history", for: credentials.bridgeURL),
              var components = URLComponents(url: base, resolvingAgainstBaseURL: false)
        else { throw BridgeHTTPError.invalidBridgeURL }
        components.queryItems = [URLQueryItem(name: "limit", value: String(min(max(limit, 1), 500)))]
        guard let url = components.url else { throw BridgeHTTPError.invalidBridgeURL }
        let data = try await perform(method: "GET", url: url, body: nil, authorization: credentials.authorizationHeader)
        guard let history = try? SignalingCoding.makeDecoder().decode(FritzBoxCallList.self, from: data) else {
            throw BridgeHTTPError.invalidResponse
        }
        return history
    }

    private func perform(method: String, url: URL, body: Data?, authorization: String?) async throws -> Data {
        try await send(method: method, url: url, body: body, authorization: authorization, headers: [:]).data
    }

    /// Sends a request; success is any 2xx and 304.
    private func send(method: String, url: URL, body: Data?, authorization: String?, headers: [String: String]) async throws -> (data: Data, response: HTTPURLResponse) {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 15
        // Never answer from URLCache: responses are per device, and a 304
        // must reach `phonebook(ifNoneMatch:)` as a 304.
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if let authorization {
            request.setValue(authorization, forHTTPHeaderField: "Authorization")
        }
        for (field, value) in headers {
            request.setValue(value, forHTTPHeaderField: field)
        }

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw BridgeHTTPError.invalidResponse }
        switch http.statusCode {
        case 200..<300, 304:
            return (data, http)
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
