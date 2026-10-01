import Foundation
import Security

public enum BridgeHTTPError: Error, Equatable, Sendable {
    case invalidBridgeURL
    case unexpectedStatus(Int)
    case invalidResponse
    /// The private listener from the pairing link did not answer: the
    /// device is not in the home network (or Tailscale is off).
    case homeNetworkRequired
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

/// The HTTPS endpoints of the bridge. The Apple Watch uses them because
/// watchOS only allows WebSocket during a CallKit call; plain HTTPS works
/// any time. Phonebook and call list are HTTPS-only on both.
///
/// Signaling v2 (ADR-0004): every request except pairing is signed with
/// the device key, and every response must carry a valid signature of the
/// pinned bridge key (errors included). Successful responses with a body
/// are sealed end to end.
///
/// Errors: `SignalingClientError.unauthorized` for 401,
/// `.clockSkew` when the bridge rejected the timestamp,
/// `.untrustedBridge` for a missing or wrong bridge signature,
/// `.bridge` when the bridge sent an `error` payload.
public struct BridgeHTTPClient: Sendable {
    private let session: URLSession
    private let keyStore: any DeviceKeyStore
    private let now: @Sendable () -> Date

    public init(
        session: URLSession = .shared,
        keyStore: any DeviceKeyStore = KeychainDeviceKeyStore(),
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.session = session
        self.keyStore = keyStore
        self.now = now
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

    // MARK: - Pairing

    /// `POST /v1/pair` (v2). Creates a new device key under a fresh tag,
    /// proves possession, and accepts the answer only if the bridge holds
    /// the key from the link's fingerprint and signed this pairing. On
    /// failure the new key is deleted again; an existing pairing and its
    /// key stay untouched.
    public func pair(link: PairingLink, deviceName: String, platform: DevicePlatform, model: String?) async throws -> BridgeCredentials {
        guard let url = Self.endpoint("pair", for: link.pairingURL) else { throw BridgeHTTPError.invalidBridgeURL }
        let tag = "device-" + UUID().uuidString.lowercased()
        let key = try keyStore.makeKey(tag: tag)
        do {
            let request = try HP2Pairing.request(
                link: link,
                deviceName: deviceName,
                platform: platform,
                model: model,
                key: key,
                nonce: try Self.randomBytes(HP2.nonceLength)
            )
            let body = try SignalingCoding.makeEncoder().encode(request)
            let (data, response): (Data, URLResponse)
            do {
                var request = makeRequest(method: "POST", url: url, body: body)
                if link.lanURL != nil { request.timeoutInterval = Self.lanPairingTimeout }
                (data, response) = try await session.data(for: request)
            } catch let error as URLError where link.lanURL != nil && Self.isUnreachable(error) {
                throw BridgeHTTPError.homeNetworkRequired
            }
            guard let http = response as? HTTPURLResponse else { throw BridgeHTTPError.invalidResponse }
            // The private listener refuses addresses outside the home network.
            if http.statusCode == 403,
               (try? SignalingCoding.makeDecoder().decode(SignalingErrorPayload.self, from: data))?.code == .homeNetworkRequired {
                throw BridgeHTTPError.homeNetworkRequired
            }
            guard (200..<300).contains(http.statusCode) else { throw Self.failure(status: http.statusCode, body: data) }
            guard let result = try? SignalingCoding.makeDecoder().decode(PairingResult.self, from: data) else {
                throw BridgeHTTPError.invalidResponse
            }
            return try HP2Pairing.credentials(from: result, request: request, link: link, keyTag: tag)
        } catch {
            try? keyStore.deleteKey(tag: tag)
            throw error
        }
    }

    // MARK: - Pairing in the home network (ADR-0007)

    /// Steps 1–3 of LAN pairing with the private listener at `lanURL`
    /// (found via Bonjour, `ws://host:port/v1/ws`). Afterwards the session
    /// holds the SAS to show; `waitForLanApproval` waits for the admin.
    /// The new device key is deleted again on failure.
    public func startLanPairing(lanURL: URL, deviceName: String, model: String?) async throws -> LanPairingSession {
        guard let startURL = Self.endpoint("pair/lan", for: lanURL) else { throw BridgeHTTPError.invalidBridgeURL }
        let tag = "device-" + UUID().uuidString.lowercased()
        let key = try keyStore.makeKey(tag: tag)
        do {
            var pairing = HP2LanPairing(key: key, deviceName: deviceName, model: model, nonce: try Self.randomBytes(HP2.nonceLength))
            let offer: LanPairOffer = try await lanPost(startURL, body: pairing.start)
            let reveal = try pairing.accept(offer, key: key)
            guard let revealURL = Self.endpoint("pair/lan/\(offer.pairingId)/reveal", for: lanURL) else { throw BridgeHTTPError.invalidBridgeURL }
            let state: LanPairState = try await lanPost(revealURL, body: reveal)
            guard state.status == .pending, let sas = pairing.sas else { throw BridgeHTTPError.invalidResponse }
            return LanPairingSession(lanURL: lanURL, keyTag: tag, pairing: pairing, sas: sas, bridgeName: offer.bridgeName, expiresAt: state.expiresAt ?? offer.expiresAt)
        } catch {
            try? keyStore.deleteKey(tag: tag)
            throw error
        }
    }

    /// Long-polls until the admin approved or denied, or the request
    /// expired. On approval the credentials are returned (not stored);
    /// otherwise the key is deleted. Cancelling the task stops waiting and
    /// deletes the key too.
    public func waitForLanApproval(_ pending: LanPairingSession) async throws -> LanPairingOutcome {
        guard let url = Self.endpoint("pair/lan/\(pending.pairing.offer?.pairingId ?? "")", for: pending.lanURL),
              var components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        else { throw BridgeHTTPError.invalidBridgeURL }
        components.queryItems = [URLQueryItem(name: "wait", value: "25")]
        guard let pollURL = components.url else { throw BridgeHTTPError.invalidBridgeURL }
        do {
            while true {
                try Task.checkCancellation()
                var request = makeRequest(method: "GET", url: pollURL, body: nil)
                request.timeoutInterval = 40
                let (data, response) = try await lanData(for: request)
                guard let http = response as? HTTPURLResponse else { throw BridgeHTTPError.invalidResponse }
                guard (200..<300).contains(http.statusCode) else { throw Self.failure(status: http.statusCode, body: data) }
                guard let state = try? SignalingCoding.makeDecoder().decode(LanPairState.self, from: data) else {
                    throw BridgeHTTPError.invalidResponse
                }
                switch state.status {
                case .pending:
                    continue
                case .approved:
                    return .approved(try pending.pairing.credentials(from: state, discoveredURL: pending.lanURL, keyTag: pending.keyTag))
                case .denied:
                    try? keyStore.deleteKey(tag: pending.keyTag)
                    return .denied
                case .expired:
                    try? keyStore.deleteKey(tag: pending.keyTag)
                    return .expired
                }
            }
        } catch {
            try? keyStore.deleteKey(tag: pending.keyTag)
            throw error
        }
    }

    /// Gives up a pairing that was not approved: deletes its key.
    public func cancelLanPairing(_ pending: LanPairingSession) {
        try? keyStore.deleteKey(tag: pending.keyTag)
    }

    private func lanData(for request: URLRequest) async throws -> (Data, URLResponse) {
        do {
            return try await session.data(for: request)
        } catch let error as URLError where Self.isUnreachable(error) {
            throw BridgeHTTPError.homeNetworkRequired
        }
    }

    private func lanPost<Body: Encodable, Answer: Decodable>(_ url: URL, body: Body) async throws -> Answer {
        var request = makeRequest(method: "POST", url: url, body: try SignalingCoding.makeEncoder().encode(body))
        request.timeoutInterval = Self.lanPairingTimeout
        let (data, response) = try await lanData(for: request)
        guard let http = response as? HTTPURLResponse else { throw BridgeHTTPError.invalidResponse }
        if http.statusCode == 403,
           (try? SignalingCoding.makeDecoder().decode(SignalingErrorPayload.self, from: data))?.code == .homeNetworkRequired {
            throw BridgeHTTPError.homeNetworkRequired
        }
        guard (200..<300).contains(http.statusCode) else { throw Self.failure(status: http.statusCode, body: data) }
        guard let answer = try? SignalingCoding.makeDecoder().decode(Answer.self, from: data) else {
            throw BridgeHTTPError.invalidResponse
        }
        return answer
    }

    /// A host in the home network answers fast or not at all.
    static let lanPairingTimeout: TimeInterval = 8

    /// Errors meaning "that address can't be reached from here".
    static func isUnreachable(_ error: URLError) -> Bool {
        switch error.code {
        case .timedOut, .cannotConnectToHost, .cannotFindHost, .networkConnectionLost,
             .notConnectedToInternet, .dnsLookupFailed:
            true
        default:
            false
        }
    }

    // MARK: - Authenticated endpoints

    /// `PUT /v1/device`: push token, capabilities, topic.
    public func updateDevice(_ update: DeviceUpdate, credentials: BridgeCredentials) async throws {
        guard let url = Self.endpoint("device", for: credentials.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        _ = try await send(method: "PUT", url: url, body: try SignalingCoding.makeEncoder().encode(update), credentials: credentials)
    }

    /// `DELETE /v1/device`: the device unpairs itself.
    public func deleteDevice(credentials: BridgeCredentials) async throws {
        guard let url = Self.endpoint("device", for: credentials.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        _ = try await send(method: "DELETE", url: url, body: nil, credentials: credentials)
    }

    /// `GET /v1/calls/{callId}`: the call from this device's point of view.
    /// A ringing watch polls this while it may not open a WebSocket. A call
    /// the bridge no longer knows (404 `call_not_found`) is reported as
    /// ended with `.notFound`.
    public func callStatus(_ callId: CallID, credentials: BridgeCredentials) async throws -> BridgeCallStatus {
        guard let url = Self.endpoint("calls/\(callId)", for: credentials.bridgeURL) else { throw BridgeHTTPError.invalidBridgeURL }
        let data: Data
        do {
            data = try await send(method: "GET", url: url, body: nil, credentials: credentials).data
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
        let (data, response) = try await send(method: "GET", url: url, body: nil, credentials: credentials, headers: headers)
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
        let data = try await send(method: "GET", url: url, body: nil, credentials: credentials).data
        guard let history = try? SignalingCoding.makeDecoder().decode(FritzBoxCallList.self, from: data) else {
            throw BridgeHTTPError.invalidResponse
        }
        return history
    }

    // MARK: - Private

    /// Signs the request, verifies the bridge's signature on the response
    /// and opens a sealed body. Success is any 2xx and 304; returns the
    /// plaintext body. With `adminKey` the request also carries the
    /// `HP2-Admin` signature (ADR-0009); signing it asks for Face ID.
    func send(
        method: String,
        url: URL,
        body: Data?,
        credentials: BridgeCredentials,
        headers: [String: String] = [:],
        adminKey: (any DeviceSigningKey)? = nil
    ) async throws -> (data: Data, response: HTTPURLResponse) {
        guard let key = try keyStore.key(tag: credentials.keyTag) else {
            // Without its key this device can't prove who it is.
            throw SignalingClientError.unauthorized
        }
        let exchange = try HP2Signer(credentials: credentials, key: key, now: now).exchange(method: method, url: url, body: body)
        var request = makeRequest(method: method, url: url, body: body)
        request.setValue(exchange.authorization.headerValue, forHTTPHeaderField: "Authorization")
        if let adminKey {
            let signature = try exchange.adminSignature(key: adminKey, method: method, pathAndQuery: HP2Signer.pathAndQuery(of: url), body: body ?? Data())
            request.setValue(signature, forHTTPHeaderField: HP2.adminHeaderName)
        }
        for (field, value) in headers {
            request.setValue(value, forHTTPHeaderField: field)
        }

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw BridgeHTTPError.invalidResponse }

        let keys: HP2SessionKeys
        do {
            keys = try exchange.verifyResponse(status: http.statusCode, bridgeHeader: http.value(forHTTPHeaderField: HP2.bridgeHeaderName), body: data)
        } catch {
            throw SignalingClientError.untrustedBridge
        }

        let plaintext: Data
        if data.isEmpty {
            plaintext = data
        } else if http.value(forHTTPHeaderField: "Content-Type")?.hasPrefix(HP2.sealedContentType) == true {
            do {
                plaintext = try exchange.openBody(data, keys: keys)
            } catch {
                throw SignalingClientError.untrustedBridge
            }
        } else if http.statusCode >= 400 {
            // Signed but unsealed: an error the bridge answered before it
            // could trust the request (e.g. 401).
            plaintext = data
        } else {
            // Content of a successful response is always sealed.
            throw SignalingClientError.untrustedBridge
        }

        switch http.statusCode {
        case 200..<300, 304:
            return (plaintext, http)
        default:
            throw Self.failure(status: http.statusCode, body: plaintext)
        }
    }

    private func makeRequest(method: String, url: URL, body: Data?) -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 15
        // Never answer from URLCache: responses are per device and signed
        // for one request, and a 304 must reach `phonebook(ifNoneMatch:)`.
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        return request
    }

    /// Maps an error status and its (plaintext) body.
    private static func failure(status: Int, body: Data) -> any Error {
        let payload = try? SignalingCoding.makeDecoder().decode(SignalingErrorPayload.self, from: body)
        if status == 401 {
            let code = payload?.code ?? (try? JSONDecoder().decode(ErrorCode.self, from: body)).map { SignalingErrorCode(rawValue: $0.code) }
            return code == .clockSkew ? SignalingClientError.clockSkew : SignalingClientError.unauthorized
        }
        if let payload { return SignalingClientError.bridge(payload) }
        return BridgeHTTPError.unexpectedStatus(status)
    }

    /// `{"code": "…"}` without a message, as in v2's 401 bodies.
    private struct ErrorCode: Decodable {
        var code: String
    }

    private static func randomBytes(_ count: Int) throws -> Data {
        var data = Data(count: count)
        let status = data.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, count, $0.baseAddress!) }
        guard status == errSecSuccess else { throw KeychainError(status: status) }
        return data
    }
}
