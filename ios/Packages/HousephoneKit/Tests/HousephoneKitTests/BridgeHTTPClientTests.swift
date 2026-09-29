import Foundation
import Testing
@testable import HousephoneKit

/// Answers requests of one test's `URLSession` without a network.
final class StubURLProtocol: URLProtocol, @unchecked Sendable {
    struct Response: Sendable {
        var status: Int
        var body: Data
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
        let http = HTTPURLResponse(url: request.url!, statusCode: response.status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
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
    let credentials = BridgeCredentials(
        bridgeURL: URL(string: "wss://phone.example.com/v1/ws")!,
        deviceId: DeviceID(UUID(uuidString: "9B1D4C2A-5E6F-4A7B-8C9D-0E1F2A3B4C5D")!),
        deviceSecret: "secret",
        bridgeId: "b",
        bridgeName: "Zuhause"
    )

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

    @Test func pairsOverHTTPS() async throws {
        let log = RequestLog()
        let result = PairingResult(deviceId: DeviceID(), deviceSecret: "s3cret", bridgeId: "b", bridgeName: "Zuhause")
        let body = try SignalingCoding.makeEncoder().encode(result)
        let client = BridgeHTTPClient(session: StubURLProtocol.session { request, requestBody in
            log.record(request, requestBody)
            return .init(status: 200, body: body)
        })
        let link = try PairingLink(string: "housephone://pair?url=wss://phone.example.com/v1/ws&code=K7P2XH9QRM")

        let paired = try await client.pair(link: link, deviceName: "Apple Watch", platform: .watchos, model: "Watch7,1")

        #expect(paired.deviceSecret == "s3cret")
        #expect(paired.bridgeURL == link.bridgeURL)
        let request = try #require(log.last)
        #expect(request.method == "POST")
        #expect(request.url.absoluteString == "https://phone.example.com/v1/pair")
        #expect(request.headers["Authorization"] == nil)
        let sent = try SignalingCoding.makeDecoder().decode(PairRequest.self, from: try #require(request.body))
        #expect(sent == PairRequest(code: "K7P2XH9QRM", deviceName: "Apple Watch", platform: .watchos, model: "Watch7,1"))
    }

    @Test func pairingErrorCarriesBridgePayload() async throws {
        let error = SignalingErrorPayload(code: .pairingInvalid, message: "Code abgelaufen")
        let body = try SignalingCoding.makeEncoder().encode(error)
        let client = BridgeHTTPClient(session: StubURLProtocol.session { _, _ in .init(status: 403, body: body) })
        let link = try PairingLink(string: "housephone://pair?url=wss://phone.example.com/v1/ws&code=K7P2XH9QRM")
        await #expect(throws: SignalingClientError.bridge(error)) {
            try await client.pair(link: link, deviceName: "Apple Watch", platform: .watchos, model: nil)
        }
    }

    @Test func updatesDeviceWithBearerToken() async throws {
        let log = RequestLog()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { request, body in
            log.record(request, body)
            return .init(status: 204, body: Data())
        })
        let update = DeviceUpdate(
            pushToken: "abcd",
            pushEnvironment: .development,
            mediaCapabilities: [.webSocketPCMA],
            pushTopic: "com.jorisconrad.housephone.watchkitapp.voip"
        )

        try await client.updateDevice(update, credentials: credentials)

        let request = try #require(log.last)
        #expect(request.method == "PUT")
        #expect(request.url.absoluteString == "https://phone.example.com/v1/device")
        #expect(request.headers["Authorization"] == "Bearer 9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d.secret")
        let object = try JSONSerialization.jsonObject(with: try #require(request.body)) as? [String: Any]
        #expect(object?["mediaCapabilities"] as? [String] == ["websocket-pcma"])
        #expect(object?["pushTopic"] as? String == "com.jorisconrad.housephone.watchkitapp.voip")
    }

    @Test func unauthorizedMapsToClientError() async {
        let client = BridgeHTTPClient(session: StubURLProtocol.session { _, _ in .init(status: 401, body: Data()) })
        await #expect(throws: SignalingClientError.unauthorized) {
            try await client.updateDevice(DeviceUpdate(), credentials: credentials)
        }
    }

    @Test func deletesDevice() async throws {
        let log = RequestLog()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { request, body in
            log.record(request, body)
            return .init(status: 204, body: Data())
        })
        try await client.deleteDevice(credentials: credentials)
        #expect(log.last?.method == "DELETE")
        #expect(log.last?.url.absoluteString == "https://phone.example.com/v1/device")
    }

    @Test func unexpectedStatusWithoutPayload() async {
        let client = BridgeHTTPClient(session: StubURLProtocol.session { _, _ in .init(status: 502, body: Data("bad gateway".utf8)) })
        await #expect(throws: BridgeHTTPError.unexpectedStatus(502)) {
            try await client.deleteDevice(credentials: credentials)
        }
    }
}
