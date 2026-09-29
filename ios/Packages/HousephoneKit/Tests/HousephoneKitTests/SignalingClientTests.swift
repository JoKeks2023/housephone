import CryptoKit
import Foundation
import Testing
@testable import HousephoneKit

// MARK: - Fakes

actor Mailbox {
    private var buffer: [WebSocketMessage] = []
    private var waiter: CheckedContinuation<WebSocketMessage, any Error>?
    private var closed = false

    func push(_ text: WebSocketMessage) {
        if let waiter {
            self.waiter = nil
            waiter.resume(returning: text)
        } else {
            buffer.append(text)
        }
    }

    func next() async throws -> WebSocketMessage {
        if !buffer.isEmpty { return buffer.removeFirst() }
        if closed { throw WebSocketTransportError.closed }
        return try await withCheckedThrowingContinuation { waiter = $0 }
    }

    /// What arrived so far, without waiting.
    func pending() -> [WebSocketMessage] { buffer }

    func close() {
        closed = true
        waiter?.resume(throwing: WebSocketTransportError.closed)
        waiter = nil
    }
}

/// One WebSocket connection. The bridge end seals with the session the
/// `FakeFactory` negotiated for it.
final class FakeTransport: WebSocketTransport, @unchecked Sendable {
    /// What the bridge sends to the device.
    let inbox = Mailbox()
    /// What the device sent to the bridge.
    let outbox = Mailbox()

    private let lock = NSLock()
    private var headers: [String: String] = [:]
    private var bridgeSession: BridgeSession?

    func accept(upgradeHeaders: [String: String], session: BridgeSession?) {
        lock.withLock {
            headers = upgradeHeaders
            bridgeSession = session
        }
    }

    var session: BridgeSession? { lock.withLock { bridgeSession } }

    func send(_ text: String) async throws {
        await outbox.push(.text(text))
    }

    func send(binary data: Data) async throws {
        await outbox.push(.binary(data))
    }

    func receive() async throws -> WebSocketMessage {
        try await inbox.next()
    }

    func sendPing() async throws {}

    func close() {
        Task { await inbox.close() }
    }

    func upgradeHeader(_ name: String) -> String? {
        lock.withLock { headers.first { $0.key.caseInsensitiveCompare(name) == .orderedSame }?.value }
    }

    /// The bridge sends a sealed signaling message.
    func bridgeSends(_ message: SignalingMessage) async throws {
        let session = try #require(session)
        await inbox.push(.binary(session.seal(HP2FrameCipher.jsonPlaintext(try SignalingCoding.encode(message)))))
    }

    /// The bridge sends a sealed audio message (`AudioFrame` layout).
    func bridgeSendsAudio(_ frame: Data) async throws {
        let session = try #require(session)
        await inbox.push(.binary(session.seal(frame)))
    }

    /// The bridge sends raw bytes, e.g. a tampered frame.
    func bridgeSendsRaw(_ message: WebSocketMessage) async {
        await inbox.push(message)
    }

    /// The next JSON message the device sent, opened with the bridge's
    /// session; skips audio. Fails on anything unsealed.
    func nextSent() async throws -> SignalingMessage {
        while true {
            let plaintext = try await nextPlaintext()
            if plaintext.first == HP2FrameType.json.rawValue {
                return try SignalingCoding.decode(String(decoding: plaintext.dropFirst(), as: UTF8.self))
            }
        }
    }

    /// The next audio message the device sent; skips JSON.
    func nextSentAudio() async throws -> Data {
        while true {
            let plaintext = try await nextPlaintext()
            if plaintext.first == HP2FrameType.audio.rawValue { return plaintext }
        }
    }

    private func nextPlaintext() async throws -> Data {
        guard case .binary(let frame) = try await outbox.next() else {
            Issue.record("device sent an unsealed text frame")
            throw HP2Error.invalidFrame
        }
        return try #require(session).open(frame)
    }
}

/// Hands out transports and plays the bridge's side of the upgrade: checks
/// the device's signature and answers with a signed `101`.
final class FakeFactory: WebSocketTransportFactory, @unchecked Sendable {
    enum Upgrade {
        case valid
        /// No `HP2-Bridge` header.
        case unsigned
        /// Signed by another key than the one the device pinned.
        case signedByImpostor
        case rejectAsUnauthorized
        case rejectAsClockSkew
    }

    private let lock = NSLock()
    private var transports: [FakeTransport]
    private let bridge: TestBridge
    private let devicePublicKey: Data?
    private let upgrade: Upgrade
    private(set) var connections: [(url: URL, headers: [String: String], signatureValid: Bool)] = []

    init(transports: [FakeTransport], bridge: TestBridge, devicePublicKey: Data? = nil, upgrade: Upgrade = .valid) {
        self.transports = transports
        self.bridge = bridge
        self.devicePublicKey = devicePublicKey
        self.upgrade = upgrade
    }

    func connect(to url: URL, headers: [String: String]) async throws -> any WebSocketTransport {
        try lock.withLock {
            let authorization = headers["Authorization"] ?? ""
            let valid = devicePublicKey.map {
                bridge.verifiesDevice(authorization: authorization, method: "GET", pathAndQuery: HP2Signer.pathAndQuery(of: url), body: Data(), devicePublicKey: $0)
            } ?? true
            connections.append((url, headers, valid))
            switch upgrade {
            case .rejectAsUnauthorized: throw WebSocketTransportError.unauthorized
            case .rejectAsClockSkew: throw WebSocketTransportError.clockSkew
            default: break
            }
            guard !transports.isEmpty else { throw WebSocketTransportError.closed }
            let transport = transports.removeFirst()
            let impostor = upgrade == .signedByImpostor ? Curve25519.Signing.PrivateKey() : nil
            guard let answer = bridge.answer(authorization: authorization, status: 101, plaintext: nil, signWith: impostor) else {
                throw WebSocketTransportError.httpStatus(400)
            }
            let upgradeHeaders = upgrade == .unsigned ? [:] : [HP2.bridgeHeaderName: answer.header]
            transport.accept(upgradeHeaders: upgradeHeaders, session: answer.session)
            return transport
        }
    }

    var connectionCount: Int { lock.withLock { connections.count } }
}

// MARK: - Tests

struct SignalingClientTests {
    let bridge = TestBridge()
    let device: TestDevice
    let hello = Hello(appVersion: "0.1.0 (1)", platform: .ios, pushToken: "abcd", pushEnvironment: .development)
    let welcome = Welcome(bridgeId: "e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e", bridgeName: "Zuhause", bridgeVersion: "0.4.0", sipRegistered: true)

    init() {
        device = TestDevice(bridge: bridge)
    }

    var fastConfiguration: SignalingClient.Configuration {
        var configuration = SignalingClient.Configuration()
        configuration.initialBackoff = .milliseconds(10)
        configuration.maximumBackoff = .milliseconds(20)
        configuration.welcomeTimeout = .seconds(2)
        return configuration
    }

    func makeClient(_ factory: FakeFactory, keyStore: (any DeviceKeyStore)? = nil) -> SignalingClient {
        SignalingClient(credentials: device.credentials, hello: hello, keyStore: keyStore ?? device.keyStore, factory: factory, configuration: fastConfiguration)
    }

    func connect(_ client: SignalingClient, _ transport: FakeTransport) async throws {
        await client.start()
        #expect(try await transport.nextSent() == .hello(hello))
        try await transport.bridgeSends(.welcome(welcome))
        _ = try await client.waitUntilConnected(timeout: .seconds(2))
    }

    @Test func connectsWithSignedUpgradeAndSealedHello() async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport], bridge: bridge, devicePublicKey: device.key.publicKeyX963)
        let client = makeClient(factory)
        await client.start()

        #expect(try await transport.nextSent() == .hello(hello))
        try await transport.bridgeSends(.welcome(welcome))
        #expect(try await client.waitUntilConnected(timeout: .seconds(2)) == welcome)

        let connection = try #require(factory.connections.first)
        #expect(connection.signatureValid)
        let header = try #require(connection.headers["Authorization"])
        let authorization = try #require(HP2Authorization(headerValue: header))
        #expect(authorization.deviceId == "9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d")
        #expect(connection.headers.values.allSatisfy { !$0.contains("Bearer") })
        await client.stop()
    }

    @Test func deliversMessagesAndTracksStatus() async throws {
        let transport = FakeTransport()
        let client = makeClient(FakeFactory(transports: [transport], bridge: bridge))
        try await connect(client, transport)

        let ended = CallEnded(callId: CallID(), reason: .busy, sipCode: 486)
        let paired = DevicePaired(deviceName: "Apple Watch", platform: .watchos, pairedAt: Date(timeIntervalSince1970: 1_800_000_000))
        try await transport.bridgeSends(.status(BridgeStatus(sipRegistered: false)))
        try await transport.bridgeSends(.callEnded(ended))
        try await transport.bridgeSends(.devicePaired(paired))

        var received: [SignalingMessage] = []
        for await event in client.events {
            if case .message(let message) = event { received.append(message) }
            if received.count == 3 { break }
        }
        #expect(received == [.status(BridgeStatus(sipRegistered: false)), .callEnded(ended), .devicePaired(paired)])
        #expect(await client.state.welcome?.sipRegistered == false)
        await client.stop()
    }

    @Test func sendingWhileDisconnectedFails() async {
        let client = makeClient(FakeFactory(transports: [], bridge: bridge))
        await #expect(throws: SignalingClientError.notConnected) {
            try await client.send(.callAttach(CallReference(callId: CallID())))
        }
    }

    @Test func unauthorizedStopsReconnecting() async throws {
        let factory = FakeFactory(transports: [], bridge: bridge, upgrade: .rejectAsUnauthorized)
        let client = makeClient(factory)
        await #expect(throws: SignalingClientError.unauthorized) {
            try await client.waitUntilConnected(timeout: .seconds(2))
        }
        #expect(await client.state == .unauthorized)
        try await Task.sleep(for: .milliseconds(100))
        #expect(factory.connectionCount == 1)
    }

    @Test func missingDeviceKeyIsUnauthorized() async throws {
        let factory = FakeFactory(transports: [FakeTransport()], bridge: bridge)
        let client = makeClient(factory, keyStore: InMemoryDeviceKeyStore())
        await #expect(throws: SignalingClientError.unauthorized) {
            try await client.waitUntilConnected(timeout: .seconds(2))
        }
        #expect(factory.connectionCount == 0)
    }

    @Test func clockSkewStopsUntilRefresh() async throws {
        let factory = FakeFactory(transports: [], bridge: bridge, upgrade: .rejectAsClockSkew)
        let client = makeClient(factory)
        await #expect(throws: SignalingClientError.clockSkew) {
            try await client.waitUntilConnected(timeout: .seconds(2))
        }
        #expect(await client.state == .clockSkew)
        try await Task.sleep(for: .milliseconds(100))
        #expect(factory.connectionCount == 1)
    }

    @Test(arguments: [FakeFactory.Upgrade.unsigned, .signedByImpostor])
    func untrustedBridgeGetsNothing(upgrade: FakeFactory.Upgrade) async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport, FakeTransport()], bridge: bridge, upgrade: upgrade)
        let client = makeClient(factory)
        await #expect(throws: SignalingClientError.untrustedBridge) {
            try await client.waitUntilConnected(timeout: .seconds(2))
        }
        #expect(await client.state == .untrustedBridge)
        // Not even hello went out, and no automatic retry.
        #expect(await transport.outbox.pending().isEmpty)
        try await Task.sleep(for: .milliseconds(100))
        #expect(factory.connectionCount == 1)
    }

    @Test func tamperedFrameDropsTheConnection() async throws {
        let first = FakeTransport()
        let second = FakeTransport()
        let factory = FakeFactory(transports: [first, second], bridge: bridge)
        let client = makeClient(factory)
        try await connect(client, first)

        // A frame sealed for this session, then flipped: never delivered.
        let session = try #require(first.session)
        var frame = session.seal(HP2FrameCipher.jsonPlaintext(try SignalingCoding.encode(.status(BridgeStatus(sipRegistered: false)))))
        frame[frame.startIndex] ^= 0x01
        await first.bridgeSendsRaw(.binary(frame))

        // The client reconnects on a fresh session.
        #expect(try await second.nextSent() == .hello(hello))
        try await second.bridgeSends(.welcome(welcome))
        var sawReconnect = false
        for await event in client.events {
            if case .message(.status) = event { Issue.record("tampered message was delivered") }
            if case .state(.waitingToReconnect) = event { sawReconnect = true }
            if case .state(.connected) = event, sawReconnect { break }
        }
        #expect(factory.connectionCount == 2)
        await client.stop()
    }

    @Test(arguments: [
        WebSocketMessage.text(#"{"type":"status","payload":{"sipRegistered":false}}"#),
        .binary(Data(repeating: 0, count: 40)),
    ])
    func unsealedOrForeignFramesDropTheConnection(message: WebSocketMessage) async throws {
        let first = FakeTransport()
        let second = FakeTransport()
        let factory = FakeFactory(transports: [first, second], bridge: bridge)
        let client = makeClient(factory)
        try await connect(client, first)

        await first.bridgeSendsRaw(message)
        #expect(try await second.nextSent() == .hello(hello))
        #expect(factory.connectionCount == 2)
        await client.stop()
    }

    @Test func replayedFrameDropsTheConnection() async throws {
        let first = FakeTransport()
        let second = FakeTransport()
        let factory = FakeFactory(transports: [first, second], bridge: bridge)
        let client = makeClient(factory)
        try await connect(client, first)

        let session = try #require(first.session)
        let frame = session.seal(HP2FrameCipher.jsonPlaintext(try SignalingCoding.encode(.status(BridgeStatus(sipRegistered: false)))))
        await first.bridgeSendsRaw(.binary(frame))
        await first.bridgeSendsRaw(.binary(frame))

        #expect(try await second.nextSent() == .hello(hello))
        await client.stop()
    }

    @Test func reconnectsAfterDrop() async throws {
        let first = FakeTransport()
        let second = FakeTransport()
        let factory = FakeFactory(transports: [first, second], bridge: bridge)
        let client = makeClient(factory)
        try await connect(client, first)

        first.close()
        #expect(try await second.nextSent() == .hello(hello))
        try await second.bridgeSends(.welcome(welcome))

        var sawReconnect = false
        for await event in client.events {
            if case .state(.waitingToReconnect) = event { sawReconnect = true }
            if case .state(.connected) = event, sawReconnect { break }
        }
        #expect(factory.connectionCount == 2)
        // Each connection signs anew.
        let nonces = factory.connections.compactMap { $0.headers["Authorization"].flatMap(HP2Authorization.init(headerValue:))?.nonce }
        #expect(Set(nonces).count == 2)
        await client.stop()
    }

    @Test func waitTimesOutWithoutWelcome() async {
        let transport = FakeTransport()
        let client = makeClient(FakeFactory(transports: [transport], bridge: bridge))
        await #expect(throws: SignalingClientError.timeout) {
            try await client.waitUntilConnected(timeout: .milliseconds(200))
        }
        await client.stop()
    }

    @Test func pushTokenChangeSendsDeviceUpdate() async throws {
        let transport = FakeTransport()
        let client = makeClient(FakeFactory(transports: [transport], bridge: bridge))
        try await connect(client, transport)

        var newHello = hello
        newHello.pushToken = "ffff"
        await client.updateHello(newHello)
        #expect(try await transport.nextSent() == .deviceUpdate(DeviceUpdate(pushToken: "ffff", pushEnvironment: .development)))
        await client.stop()
    }

    @Test func audioTravelsBothWaysSealed() async throws {
        let transport = FakeTransport()
        let client = makeClient(FakeFactory(transports: [transport], bridge: bridge))
        try await connect(client, transport)

        let fromBridge = try #require(AudioFrame.encode(aLaw: Data(repeating: 0x2A, count: 160)))
        try await transport.bridgeSendsAudio(fromBridge)
        // JSON keeps flowing on its own stream while audio arrives.
        try await transport.bridgeSends(.status(BridgeStatus(sipRegistered: true)))

        var iterator = client.audio.makeAsyncIterator()
        let received = await iterator.next()
        #expect(received == fromBridge)

        let toBridge = try #require(AudioFrame.encode(aLaw: AudioFrame.silence))
        try await client.sendAudio(toBridge)
        #expect(try await transport.nextSentAudio() == toBridge)
        await client.stop()
    }

    @Test func sendingAudioWhileDisconnectedFails() async {
        let client = makeClient(FakeFactory(transports: [], bridge: bridge))
        await #expect(throws: SignalingClientError.notConnected) {
            try await client.sendAudio(Data([0x01]))
        }
    }

    @Test func capabilityChangeSendsDeviceUpdate() async throws {
        let transport = FakeTransport()
        let client = makeClient(FakeFactory(transports: [transport], bridge: bridge))
        try await connect(client, transport)

        var newHello = hello
        newHello.mediaCapabilities = [.webRTC]
        newHello.pushTopic = "com.jorisconrad.housephone.voip"
        await client.updateHello(newHello)
        #expect(try await transport.nextSent() == .deviceUpdate(DeviceUpdate(
            pushToken: "abcd",
            pushEnvironment: .development,
            mediaCapabilities: [.webRTC],
            pushTopic: "com.jorisconrad.housephone.voip"
        )))
        await client.stop()
    }

    @Test func clockSkewIsDetectedFromTheDateHeader() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        #expect(!URLSessionWebSocketFactory.isClockSkewed(serverDate: "Fri, 15 Jan 2027 08:00:00 GMT", now: now))
        #expect(URLSessionWebSocketFactory.isClockSkewed(serverDate: "Fri, 15 Jan 2027 08:05:00 GMT", now: now))
        let close = DateFormatter()
        close.locale = Locale(identifier: "en_US_POSIX")
        close.timeZone = TimeZone(identifier: "GMT")
        close.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
        #expect(!URLSessionWebSocketFactory.isClockSkewed(serverDate: close.string(from: now.addingTimeInterval(30)), now: now))
        #expect(URLSessionWebSocketFactory.isClockSkewed(serverDate: close.string(from: now.addingTimeInterval(-90)), now: now))
        #expect(!URLSessionWebSocketFactory.isClockSkewed(serverDate: nil, now: now))
        #expect(!URLSessionWebSocketFactory.isClockSkewed(serverDate: "garbage", now: now))
    }
}
