import CryptoKit
import Foundation
import Testing
@testable import HousephoneKit

/// Answers requests of one test's `URLSession` without a network.
final class StubURLProtocol: URLProtocol, @unchecked Sendable {
    struct Response: Sendable {
        var status: Int
        var body: Data
        var headers: [String: String] = [:]
    }

    typealias Handler = @Sendable (URLRequest, Data?) -> Response

    private static let lock = NSLock()
    nonisolated(unsafe) private static var handlers: [String: Handler] = [:]

    /// A session whose requests go to `handler`.
    static func session(_ handler: @escaping Handler) -> URLSession {
        let id = UUID().uuidString
        lock.withLock { handlers[id] = handler }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        configuration.httpAdditionalHeaders = ["X-Stub-ID": id]
        return URLSession(configuration: configuration)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let id = request.value(forHTTPHeaderField: "X-Stub-ID") ?? ""
        let handler = Self.lock.withLock { Self.handlers[id] }
        let body = request.httpBody ?? request.httpBodyStream.map(Self.read)
        let response = handler?(request, body) ?? Response(status: 500, body: Data())
        let headers = ["Content-Type": "application/json"].merging(response.headers) { _, new in new }
        let http = HTTPURLResponse(url: request.url!, statusCode: response.status, httpVersion: "HTTP/1.1", headerFields: headers)!
        client?.urlProtocol(self, didReceive: http, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: response.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private static func read(_ stream: InputStream) -> Data {
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            guard count > 0 else { break }
            data.append(buffer, count: count)
        }
        return data
    }
}

/// Records what the stub saw, for assertions after the request.
final class RequestLog: @unchecked Sendable {
    private let lock = NSLock()
    private var entries: [(method: String, url: URL, headers: [String: String], body: Data?)] = []

    func record(_ request: URLRequest, _ body: Data?) {
        lock.withLock {
            entries.append((request.httpMethod ?? "", request.url!, request.allHTTPHeaderFields ?? [:], body))
        }
    }

    var last: (method: String, url: URL, headers: [String: String], body: Data?)? { lock.withLock { entries.last } }
}

struct BridgeHTTPClientTests {
    let bridge = TestBridge()
    let device: TestDevice

    init() {
        device = TestDevice(bridge: bridge)
    }

    func client(_ handler: @escaping StubURLProtocol.Handler) -> BridgeHTTPClient {
        BridgeHTTPClient(session: StubURLProtocol.session(handler), keyStore: device.keyStore)
    }

    @Test(arguments: [
        ("wss://phone.example.com/v1/ws", "https://phone.example.com/v1/pair"),
        ("wss://phone.example.com/v1/ws/", "https://phone.example.com/v1/pair"),
        ("wss://example.com/housephone/v1/ws", "https://example.com/housephone/v1/pair"),
        ("ws://192.168.0.10:8080/v1/ws", "http://192.168.0.10:8080/v1/pair"),
        ("wss://phone.example.com", "https://phone.example.com/v1/pair"),
    ])
    func derivesEndpoints(bridge: String, expected: String) {
        #expect(BridgeHTTPClient.endpoint("pair", for: URL(string: bridge)!)?.absoluteString == expected)
    }

    @Test func rejectsOtherSchemes() {
        #expect(BridgeHTTPClient.endpoint("pair", for: URL(string: "ftp://x/v1/ws")!) == nil)
    }

    @Test func pathAndQueryAsSent() {
        #expect(HP2Signer.pathAndQuery(of: URL(string: "https://h/v1/history?limit=100")!) == "/v1/history?limit=100")
        #expect(HP2Signer.pathAndQuery(of: URL(string: "wss://h/prefix/v1/ws")!) == "/prefix/v1/ws")
        #expect(HP2Signer.pathAndQuery(of: URL(string: "https://h")!) == "/")
    }

    // MARK: - Pairing v2

    /// A bridge that checks the proof and answers like `POST /v1/pair` v2.
    func pairingHandler(signWith key: Curve25519.Signing.PrivateKey? = nil, log: RequestLog? = nil) -> StubURLProtocol.Handler {
        { [bridge] request, body in
            log?.record(request, body)
            guard let body, let pair = try? SignalingCoding.makeDecoder().decode(PairRequest.self, from: body),
                  let publicKey = HP2.data(base64URL: pair.publicKey),
                  let deviceKey = try? P256.Signing.PublicKey(x963Representation: publicKey),
                  let proof = HP2.data(base64URL: pair.proof).flatMap({ try? P256.Signing.ECDSASignature(rawRepresentation: $0) }),
                  deviceKey.isValidSignature(proof, for: HP2.pairProofMessage(code: pair.code, nonce: pair.nonce, publicKey: pair.publicKey))
            else {
                return .init(status: 403, body: Data(#"{"code":"pairing_invalid","message":"proof"}"#.utf8))
            }
            let deviceId = "0b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
            let signer = key ?? bridge.signingKey
            let message = HP2.pairResponseMessage(bridgeId: bridge.bridgeId, deviceId: deviceId, publicKey: pair.publicKey, nonce: pair.nonce, code: pair.code)
            let result = PairingResult(
                deviceId: DeviceID(string: deviceId)!,
                bridgeId: bridge.bridgeId,
                bridgeName: "Zuhause",
                bridgePublicKey: HP2.base64URL(signer.publicKey.rawRepresentation),
                signature: HP2.base64URL(try! signer.signature(for: message))
            )
            return .init(status: 200, body: try! SignalingCoding.makeEncoder().encode(result))
        }
    }

    @Test func pairsAndPinsTheBridge() async throws {
        let log = RequestLog()
        let keyStore = InMemoryDeviceKeyStore()
        let client = BridgeHTTPClient(session: StubURLProtocol.session(pairingHandler(log: log)), keyStore: keyStore)
        let link = pairingLink(for: bridge)

        let paired = try await client.pair(link: link, deviceName: "Apple Watch", platform: .watchos, model: "Watch7,1")

        #expect(paired.bridgePublicKey == bridge.publicKey)
        #expect(paired.bridgeFingerprint == link.fingerprint)
        #expect(paired.bridgeURL == link.bridgeURL)
        #expect(paired.deviceId.description == "0b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d")
        // The key under the new tag signs for this device.
        let key = try #require(try keyStore.key(tag: paired.keyTag))
        let request = try #require(log.last)
        #expect(request.method == "POST")
        #expect(request.url.absoluteString == "https://phone.example.com/v1/pair")
        #expect(request.headers["Authorization"] == nil)
        let sent = try SignalingCoding.makeDecoder().decode(PairRequest.self, from: try #require(request.body))
        #expect(sent.code == "K7P2XH9QRMW4DZT8")
        #expect(sent.publicKey == HP2.base64URL(key.publicKeyX963))
        #expect(HP2.data(base64URL: sent.nonce)?.count == 16)
        #expect(sent.platform == .watchos && sent.model == "Watch7,1")
    }

    @Test func pairingWithAnotherBridgeFailsAndLeavesNoKey() async throws {
        let keyStore = InMemoryDeviceKeyStore()
        // Someone answers with a valid-looking pairing, signed by their own key.
        let client = BridgeHTTPClient(session: StubURLProtocol.session(pairingHandler(signWith: .init())), keyStore: keyStore)
        await #expect(throws: HP2Error.bridgeIdentityMismatch) {
            try await client.pair(link: pairingLink(for: bridge), deviceName: "iPhone", platform: .ios, model: nil)
        }
        #expect(keyStore.tags.isEmpty)
    }

    @Test func pairingErrorCarriesBridgePayload() async throws {
        let error = SignalingErrorPayload(code: .pairingInvalid, message: "Code abgelaufen")
        let body = try SignalingCoding.makeEncoder().encode(error)
        let keyStore = InMemoryDeviceKeyStore()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { _, _ in .init(status: 403, body: body) }, keyStore: keyStore)
        await #expect(throws: SignalingClientError.bridge(error)) {
            try await client.pair(link: pairingLink(for: bridge), deviceName: "Apple Watch", platform: .watchos, model: nil)
        }
        #expect(keyStore.tags.isEmpty)
    }

    // MARK: - Signed requests, signed and sealed responses

    @Test func updatesDeviceWithSignedRequest() async throws {
        let log = RequestLog()
        let verified = RequestLog()
        let client = client { [bridge, device] request, body in
            log.record(request, body)
            if bridge.verifiesDevice(
                authorization: request.value(forHTTPHeaderField: "Authorization") ?? "",
                method: "PUT", pathAndQuery: "/v1/device", body: body ?? Data(), devicePublicKey: device.key.publicKeyX963
            ) {
                verified.record(request, body)
            }
            return bridge.stubResponse(for: request, status: 204)
        }
        let update = DeviceUpdate(
            pushToken: "abcd",
            pushEnvironment: .development,
            mediaCapabilities: [.webSocketPCMA],
            pushTopic: "com.jorisconrad.housephone.watchkitapp.voip"
        )

        try await client.updateDevice(update, credentials: device.credentials)

        let request = try #require(log.last)
        #expect(verified.last != nil, "the bridge could verify the device signature")
        #expect(request.method == "PUT")
        #expect(request.url.absoluteString == "https://phone.example.com/v1/device")
        #expect(request.headers["Authorization"]?.hasPrefix("HP2 id=9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d") == true)
        let object = try JSONSerialization.jsonObject(with: try #require(request.body)) as? [String: Any]
        #expect(object?["mediaCapabilities"] as? [String] == ["websocket-pcma"])
        #expect(object?["pushTopic"] as? String == "com.jorisconrad.housephone.watchkitapp.voip")
    }

    @Test func unsignedResponseIsUntrusted() async {
        let client = client { _, _ in .init(status: 204, body: Data()) }
        await #expect(throws: SignalingClientError.untrustedBridge) {
            try await client.updateDevice(DeviceUpdate(), credentials: device.credentials)
        }
    }

    @Test func responseSignedByAnotherKeyIsUntrusted() async {
        let impostor = TestBridge(bridgeId: bridge.bridgeId)
        let client = client { request, _ in impostor.stubResponse(for: request, status: 204) }
        await #expect(throws: SignalingClientError.untrustedBridge) {
            try await client.updateDevice(DeviceUpdate(), credentials: device.credentials)
        }
    }

    @Test func unsealedSuccessBodyIsUntrusted() async throws {
        let json = Data(#"{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","state":"connected"}"#.utf8)
        let client = client { [bridge] request, _ in
            // Correctly signed, but the body is plaintext.
            let authorization = request.value(forHTTPHeaderField: "Authorization")!
            let answer = bridge.answer(authorization: authorization, status: 200, plaintext: json, seal: false)!
            return .init(status: 200, body: answer.body, headers: [HP2.bridgeHeaderName: answer.header])
        }
        await #expect(throws: SignalingClientError.untrustedBridge) {
            try await client.callStatus(Self.callId, credentials: device.credentials)
        }
    }

    @Test func unauthorizedMapsToClientError() async {
        let client = client { [bridge] request, _ in
            bridge.stubResponse(for: request, status: 401, json: Data(#"{"code":"unauthorized"}"#.utf8))
        }
        await #expect(throws: SignalingClientError.unauthorized) {
            try await client.updateDevice(DeviceUpdate(), credentials: device.credentials)
        }
    }

    @Test func clockSkewIsReported() async {
        let client = client { [bridge] request, _ in
            bridge.stubResponse(for: request, status: 401, json: Data(#"{"code":"clock_skew"}"#.utf8))
        }
        await #expect(throws: SignalingClientError.clockSkew) {
            try await client.updateDevice(DeviceUpdate(), credentials: device.credentials)
        }
    }

    @Test func missingKeyIsUnauthorized() async {
        let client = BridgeHTTPClient(session: StubURLProtocol.session { _, _ in .init(status: 204, body: Data()) }, keyStore: InMemoryDeviceKeyStore())
        await #expect(throws: SignalingClientError.unauthorized) {
            try await client.updateDevice(DeviceUpdate(), credentials: device.credentials)
        }
    }

    @Test func deletesDevice() async throws {
        let log = RequestLog()
        let client = client { [bridge] request, body in
            log.record(request, body)
            return bridge.stubResponse(for: request, status: 204)
        }
        try await client.deleteDevice(credentials: device.credentials)
        #expect(log.last?.method == "DELETE")
        #expect(log.last?.url.absoluteString == "https://phone.example.com/v1/device")
    }

    @Test func unexpectedStatusWithoutPayload() async {
        let client = client { [bridge] request, _ in bridge.stubResponse(for: request, status: 502, json: Data("bad gateway".utf8)) }
        await #expect(throws: BridgeHTTPError.unexpectedStatus(502)) {
            try await client.deleteDevice(credentials: device.credentials)
        }
    }

    // MARK: - GET /v1/calls/{callId}

    static let callId = CallID(UUID(uuidString: "3F0C2B4E-8A1D-4C6E-9B7A-2D5E8F1A0C93")!)

    @Test(arguments: [
        (#"{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","state":"ringing"}"#, BridgeCallStatus.State.ringing),
        (#"{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","state":"connected"}"#, .connected),
        (#"{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","state":"ended","reason":"answered_elsewhere"}"#, .ended(.answeredElsewhere, sipCode: nil)),
        (#"{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","state":"ended","reason":"busy","sipCode":486}"#, .ended(.busy, sipCode: 486)),
        (#"{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","state":"ended"}"#, .ended(.failed, sipCode: nil)),
        (#"{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","state":"on_hold"}"#, .ringing),
    ])
    func callStatus(body: String, expected: BridgeCallStatus.State) async throws {
        let log = RequestLog()
        let client = client { [bridge] request, requestBody in
            log.record(request, requestBody)
            return bridge.stubResponse(for: request, status: 200, json: Data(body.utf8))
        }
        let status = try await client.callStatus(Self.callId, credentials: device.credentials)
        #expect(status == BridgeCallStatus(callId: Self.callId, state: expected))
        #expect(log.last?.method == "GET")
        #expect(log.last?.url.absoluteString == "https://phone.example.com/v1/calls/3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93")
    }

    @Test func unknownCallIsReportedAsEnded() async throws {
        // A 404 after successful auth: sealed like any other body.
        let client = client { [bridge] request, _ in
            let authorization = request.value(forHTTPHeaderField: "Authorization")!
            let answer = bridge.answer(authorization: authorization, status: 404, plaintext: Data(#"{"code":"call_not_found","message":"unknown call"}"#.utf8))!
            return .init(status: 404, body: answer.body, headers: [HP2.bridgeHeaderName: answer.header, "Content-Type": HP2.sealedContentType])
        }
        let status = try await client.callStatus(Self.callId, credentials: device.credentials)
        #expect(status.state == .ended(.notFound, sipCode: nil))
    }

    @Test func callStatusKeepsOtherErrors() async {
        let client = client { [bridge] request, _ in bridge.stubResponse(for: request, status: 401) }
        await #expect(throws: SignalingClientError.unauthorized) {
            try await client.callStatus(Self.callId, credentials: device.credentials)
        }
    }
}
