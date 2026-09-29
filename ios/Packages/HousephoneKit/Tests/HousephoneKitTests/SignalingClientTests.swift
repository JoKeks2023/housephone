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

    func close() {
        closed = true
        waiter?.resume(throwing: WebSocketTransportError.closed)
        waiter = nil
    }
}

final class FakeTransport: WebSocketTransport {
    /// What the bridge sends to the device.
    let inbox = Mailbox()
    /// What the device sent to the bridge.
    let outbox = Mailbox()

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

    func bridgeSends(_ message: SignalingMessage) async throws {
        await inbox.push(.text(try SignalingCoding.encode(message)))
    }

    func bridgeSendsBinary(_ data: Data) async {
        await inbox.push(.binary(data))
    }

    /// The next JSON message the device sent; skips binary audio.
    func nextSent() async throws -> SignalingMessage {
        while true {
            if case .text(let text) = try await outbox.next() {
                return try SignalingCoding.decode(text)
            }
        }
    }

    /// The next binary message the device sent; skips JSON.
    func nextSentBinary() async throws -> Data {
        while true {
            if case .binary(let data) = try await outbox.next() { return data }
        }
    }
}

final class FakeFactory: WebSocketTransportFactory, @unchecked Sendable {
    private let lock = NSLock()
    private var transports: [FakeTransport]
    private var rejectAsUnauthorized: Bool
    private(set) var connections: [(url: URL, headers: [String: String])] = []

    init(transports: [FakeTransport], rejectAsUnauthorized: Bool = false) {
        self.transports = transports
        self.rejectAsUnauthorized = rejectAsUnauthorized
    }

    func connect(to url: URL, headers: [String: String]) async throws -> any WebSocketTransport {
        try lock.withLock {
            connections.append((url, headers))
            if rejectAsUnauthorized { throw WebSocketTransportError.unauthorized }
            guard !transports.isEmpty else { throw WebSocketTransportError.closed }
            return transports.removeFirst()
        }
    }

    var connectionCount: Int { lock.withLock { connections.count } }
}

// MARK: - Tests

struct SignalingClientTests {
    let credentials = BridgeCredentials(
        bridgeURL: URL(string: "wss://bridge.example/v1/ws")!,
        deviceId: DeviceID(UUID(uuidString: "9B1D4C2A-5E6F-4A7B-8C9D-0E1F2A3B4C5D")!),
        deviceSecret: "secret",
        bridgeId: "b",
        bridgeName: "Zuhause"
    )
    let hello = Hello(appVersion: "0.1.0 (1)", platform: .ios, pushToken: "abcd", pushEnvironment: .development)
    let welcome = Welcome(bridgeId: "b", bridgeName: "Zuhause", bridgeVersion: "0.1.0", sipRegistered: true)

    var fastConfiguration: SignalingClient.Configuration {
        var configuration = SignalingClient.Configuration()
        configuration.initialBackoff = .milliseconds(10)
        configuration.maximumBackoff = .milliseconds(20)
        configuration.welcomeTimeout = .seconds(2)
        return configuration
    }

    @Test func connectsWithBearerTokenAndHello() async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport])
        let client = SignalingClient(credentials: credentials, hello: hello, factory: factory, configuration: fastConfiguration)
        await client.start()

        #expect(try await transport.nextSent() == .hello(hello))
        try await transport.bridgeSends(.welcome(welcome))
        #expect(try await client.waitUntilConnected(timeout: .seconds(2)) == welcome)

        let headers = try #require(factory.connections.first?.headers)
        #expect(headers["Authorization"] == "Bearer 9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d.secret")
        await client.stop()
    }

    @Test func deliversMessagesAndTracksStatus() async throws {
        let transport = FakeTransport()
        let client = SignalingClient(credentials: credentials, hello: hello, factory: FakeFactory(transports: [transport]), configuration: fastConfiguration)
        await client.start()
        _ = try await transport.nextSent()
        try await transport.bridgeSends(.welcome(welcome))
        _ = try await client.waitUntilConnected(timeout: .seconds(2))

        let ended = CallEnded(callId: CallID(), reason: .busy, sipCode: 486)
        try await transport.bridgeSends(.status(BridgeStatus(sipRegistered: false)))
        try await transport.bridgeSends(.callEnded(ended))

        var received: [SignalingMessage] = []
        for await event in client.events {
            if case .message(let message) = event { received.append(message) }
            if received.count == 2 { break }
        }
        #expect(received == [.status(BridgeStatus(sipRegistered: false)), .callEnded(ended)])
        #expect(await client.state.welcome?.sipRegistered == false)
        await client.stop()
    }

    @Test func sendingWhileDisconnectedFails() async {
        let client = SignalingClient(credentials: credentials, hello: hello, factory: FakeFactory(transports: []), configuration: fastConfiguration)
        await #expect(throws: SignalingClientError.notConnected) {
            try await client.send(.callAttach(CallReference(callId: CallID())))
        }
    }

    @Test func unauthorizedStopsReconnecting() async throws {
        let factory = FakeFactory(transports: [], rejectAsUnauthorized: true)
        let client = SignalingClient(credentials: credentials, hello: hello, factory: factory, configuration: fastConfiguration)
        await #expect(throws: SignalingClientError.unauthorized) {
            try await client.waitUntilConnected(timeout: .seconds(2))
        }
        #expect(await client.state == .unauthorized)
        try await Task.sleep(for: .milliseconds(100))
        #expect(factory.connectionCount == 1)
    }

    @Test func reconnectsAfterDrop() async throws {
        let first = FakeTransport()
        let second = FakeTransport()
        let factory = FakeFactory(transports: [first, second])
        let client = SignalingClient(credentials: credentials, hello: hello, factory: factory, configuration: fastConfiguration)
        await client.start()

        _ = try await first.nextSent()
        try await first.bridgeSends(.welcome(welcome))
        _ = try await client.waitUntilConnected(timeout: .seconds(2))

        first.close()
        #expect(try await second.nextSent() == .hello(hello))
        try await second.bridgeSends(.welcome(welcome))

        var sawReconnect = false
        for await event in client.events {
            if case .state(.waitingToReconnect) = event { sawReconnect = true }
            if case .state(.connected) = event, sawReconnect { break }
        }
        #expect(factory.connectionCount == 2)
        await client.stop()
    }

    @Test func waitTimesOutWithoutWelcome() async {
        let transport = FakeTransport()
        let client = SignalingClient(credentials: credentials, hello: hello, factory: FakeFactory(transports: [transport]), configuration: fastConfiguration)
        await #expect(throws: SignalingClientError.timeout) {
            try await client.waitUntilConnected(timeout: .milliseconds(200))
        }
        await client.stop()
    }

    @Test func pushTokenChangeSendsDeviceUpdate() async throws {
        let transport = FakeTransport()
        let client = SignalingClient(credentials: credentials, hello: hello, factory: FakeFactory(transports: [transport]), configuration: fastConfiguration)
        await client.start()
        _ = try await transport.nextSent()
        try await transport.bridgeSends(.welcome(welcome))
        _ = try await client.waitUntilConnected(timeout: .seconds(2))

        var newHello = hello
        newHello.pushToken = "ffff"
        await client.updateHello(newHello)
        #expect(try await transport.nextSent() == .deviceUpdate(DeviceUpdate(pushToken: "ffff", pushEnvironment: .development)))
        await client.stop()
    }

    @Test func audioTravelsBothWaysAsBinaryMessages() async throws {
        let transport = FakeTransport()
        let client = SignalingClient(credentials: credentials, hello: hello, factory: FakeFactory(transports: [transport]), configuration: fastConfiguration)
        await client.start()
        _ = try await transport.nextSent()
        try await transport.bridgeSends(.welcome(welcome))
        _ = try await client.waitUntilConnected(timeout: .seconds(2))

        let fromBridge = try #require(AudioFrame.encode(aLaw: Data(repeating: 0x2A, count: 160)))
        await transport.bridgeSendsBinary(fromBridge)
        // JSON keeps flowing on its own stream while audio arrives.
        try await transport.bridgeSends(.status(BridgeStatus(sipRegistered: true)))

        var iterator = client.audio.makeAsyncIterator()
        let received = await iterator.next()
        #expect(received == fromBridge)

        let toBridge = try #require(AudioFrame.encode(aLaw: AudioFrame.silence))
        try await client.sendAudio(toBridge)
        #expect(try await transport.nextSentBinary() == toBridge)
        await client.stop()
    }

    @Test func sendingAudioWhileDisconnectedFails() async {
        let client = SignalingClient(credentials: credentials, hello: hello, factory: FakeFactory(transports: []), configuration: fastConfiguration)
        await #expect(throws: SignalingClientError.notConnected) {
            try await client.sendAudio(Data([0x01]))
        }
    }

    @Test func capabilityChangeSendsDeviceUpdate() async throws {
        let transport = FakeTransport()
        let client = SignalingClient(credentials: credentials, hello: hello, factory: FakeFactory(transports: [transport]), configuration: fastConfiguration)
        await client.start()
        _ = try await transport.nextSent()
        try await transport.bridgeSends(.welcome(welcome))
        _ = try await client.waitUntilConnected(timeout: .seconds(2))

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

    // MARK: Pairing

    @Test func pairingReturnsCredentials() async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport])
        let link = try PairingLink(string: "housephone://pair?url=wss://bridge.example/v1/ws&code=K7P2XH9QRM")
        let result = PairingResult(deviceId: DeviceID(), deviceSecret: "s3cret", bridgeId: "b", bridgeName: "Zuhause")

        async let credentials = SignalingClient.pair(link: link, deviceName: "iPhone", platform: .ios, model: "iPhone17,1", factory: factory)
        #expect(try await transport.nextSent() == .pair(PairRequest(code: "K7P2XH9QRM", deviceName: "iPhone", platform: .ios, model: "iPhone17,1")))
        try await transport.bridgeSends(.pairOK(result))

        let paired = try await credentials
        #expect(paired.bridgeURL == link.bridgeURL)
        #expect(paired.deviceSecret == "s3cret")
        #expect(factory.connections.first?.headers.isEmpty == true)
    }

    @Test func pairingSurfacesBridgeError() async throws {
        let transport = FakeTransport()
        let link = try PairingLink(string: "housephone://pair?url=wss://bridge.example/v1/ws&code=K7P2XH9QRM")
        let error = SignalingErrorPayload(code: .pairingInvalid, message: "Code abgelaufen")

        let pairing = Task {
            try await SignalingClient.pair(link: link, deviceName: "iPhone", platform: .ios, model: nil, factory: FakeFactory(transports: [transport]))
        }
        _ = try await transport.nextSent()
        try await transport.bridgeSends(.error(error))
        await #expect(throws: SignalingClientError.bridge(error)) { try await pairing.value }
    }
}
