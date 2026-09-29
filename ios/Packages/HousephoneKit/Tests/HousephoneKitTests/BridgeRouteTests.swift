import Foundation
import Testing
@testable import HousephoneKit

/// Answers probes from a settable set of reachable hosts.
final class StubProbe: ReachabilityProbe, @unchecked Sendable {
    private let lock = NSLock()
    private var reachable: Set<String>
    private var probed: [URL] = []

    init(reachable: Set<String> = []) {
        self.reachable = reachable
    }

    func setReachable(_ hosts: Set<String>) {
        lock.withLock { reachable = hosts }
    }

    var probes: [URL] { lock.withLock { probed } }

    func canConnect(to url: URL, timeout: Duration) async -> Bool {
        lock.withLock {
            probed.append(url)
            return reachable.contains(url.host() ?? "")
        }
    }
}

struct BridgeRouteChooserTests {
    let publicURL = URL(string: "wss://phone.example.com/v1/ws")!
    let lanURL = URL(string: "ws://192.168.178.20:8081/v1/ws")!
    let wifi = NetworkPathInfo(isSatisfied: true, usesLocalNetwork: true, usesVPN: false)
    let cellular = NetworkPathInfo(isSatisfied: true, usesLocalNetwork: false, usesVPN: false)
    let cellularWithTailscale = NetworkPathInfo(isSatisfied: true, usesLocalNetwork: false, usesVPN: true)

    @Test func prefersTheLanListenerWhenItAnswers() async {
        let chooser = BridgeRouteChooser(probe: StubProbe(reachable: ["192.168.178.20"]))
        #expect(await chooser.url(publicURL: publicURL, lanURL: lanURL, path: wifi) == lanURL)
        #expect(await chooser.url(publicURL: publicURL, lanURL: lanURL, path: nil) == lanURL)
    }

    @Test func fallsBackToPublicWhenTheLanListenerIsSilent() async {
        let probe = StubProbe()
        let chooser = BridgeRouteChooser(probe: probe)
        #expect(await chooser.url(publicURL: publicURL, lanURL: lanURL, path: wifi) == publicURL)
        #expect(probe.probes == [lanURL])
    }

    @Test func cellularWithoutVPNSkipsTheProbe() async {
        let probe = StubProbe(reachable: ["192.168.178.20"])
        let chooser = BridgeRouteChooser(probe: probe)
        #expect(await chooser.url(publicURL: publicURL, lanURL: lanURL, path: cellular) == publicURL)
        #expect(probe.probes.isEmpty)
    }

    @Test func tailscaleOnCellularProbes() async {
        let probe = StubProbe(reachable: ["192.168.178.20"])
        let chooser = BridgeRouteChooser(probe: probe)
        #expect(await chooser.url(publicURL: publicURL, lanURL: lanURL, path: cellularWithTailscale) == lanURL)
    }

    @Test func withoutLanURLAlwaysPublic() async {
        let probe = StubProbe(reachable: ["phone.example.com"])
        let chooser = BridgeRouteChooser(probe: probe)
        #expect(await chooser.url(publicURL: publicURL, lanURL: nil, path: wifi) == publicURL)
        #expect(probe.probes.isEmpty)
    }
}

struct SignalingClientRouteTests {
    let bridge = TestBridge()
    let device: TestDevice
    let hello = Hello(appVersion: "0.1.0 (1)", platform: .ios)
    let publicURL = URL(string: "wss://phone.example.com/v1/ws")!
    let lanURL = URL(string: "ws://192.168.178.20:8081/v1/ws")!
    let wifi = NetworkPathInfo(isSatisfied: true, usesLocalNetwork: true, usesVPN: false)
    let cellular = NetworkPathInfo(isSatisfied: true, usesLocalNetwork: false, usesVPN: false)

    init() {
        device = TestDevice(bridge: bridge)
    }

    func welcome(lan: URL?) -> Welcome {
        Welcome(bridgeId: bridge.bridgeId, bridgeName: "Zuhause", bridgeVersion: "0.5.0", sipRegistered: true, lanUrl: lan)
    }

    func makeClient(_ factory: FakeFactory, probe: StubProbe, lan: URL?) -> SignalingClient {
        var credentials = device.credentials
        credentials.lanURL = lan
        var configuration = SignalingClient.Configuration()
        configuration.initialBackoff = .seconds(30)
        configuration.maximumBackoff = .seconds(30)
        configuration.welcomeTimeout = .seconds(2)
        return SignalingClient(
            credentials: credentials, hello: hello, keyStore: device.keyStore, factory: factory,
            route: BridgeRouteChooser(probe: probe), configuration: configuration
        )
    }

    /// Answers hello with welcome and waits until the client reports it.
    func accept(_ transport: FakeTransport, on client: SignalingClient, welcome: Welcome) async throws {
        #expect(try await transport.nextSent() == .hello(hello))
        try await transport.bridgeSends(.welcome(welcome))
        for await event in client.events {
            if case .state(.connected) = event { break }
        }
    }

    @Test func connectsOverTheLanListenerAtHome() async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport], bridge: bridge, devicePublicKey: device.key.publicKeyX963)
        let client = makeClient(factory, probe: StubProbe(reachable: ["192.168.178.20"]), lan: lanURL)
        await client.networkPathChanged(wifi)
        await client.start()
        try await accept(transport, on: client, welcome: welcome(lan: lanURL))

        let connection = try #require(factory.connections.first)
        #expect(connection.url == lanURL)
        // Same HP2 signature and pinning as over the public URL.
        #expect(connection.signatureValid)
        #expect(await client.activeURL == lanURL)
        await client.stop()
    }

    @Test func usesThePublicURLAway() async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport], bridge: bridge)
        let client = makeClient(factory, probe: StubProbe(), lan: lanURL)
        await client.start()
        try await accept(transport, on: client, welcome: welcome(lan: lanURL))
        #expect(factory.connections.first?.url == publicURL)
        await client.stop()
    }

    @Test func switchesToTheLanListenerWhenJoiningHomeWiFi() async throws {
        let away = FakeTransport()
        let home = FakeTransport()
        let factory = FakeFactory(transports: [away, home], bridge: bridge)
        let probe = StubProbe()
        // Paired before the bridge had a private listener: the URL comes
        // from welcome.
        let client = makeClient(factory, probe: probe, lan: nil)
        await client.networkPathChanged(cellular)
        await client.start()
        try await accept(away, on: client, welcome: welcome(lan: lanURL))
        #expect(await client.lanURL == lanURL)

        probe.setReachable(["192.168.178.20"])
        await client.networkPathChanged(wifi)
        // Reconnects right away (the backoff is 30 s) over the LAN.
        try await accept(home, on: client, welcome: welcome(lan: lanURL))
        #expect(factory.connections.map(\.url) == [publicURL, lanURL])
        #expect(await client.activeURL == lanURL)
        await client.stop()
    }

    @Test func switchesToPublicWhenLeavingHome() async throws {
        let home = FakeTransport()
        let away = FakeTransport()
        let factory = FakeFactory(transports: [home, away], bridge: bridge)
        let probe = StubProbe(reachable: ["192.168.178.20"])
        let client = makeClient(factory, probe: probe, lan: lanURL)
        await client.networkPathChanged(wifi)
        await client.start()
        try await accept(home, on: client, welcome: welcome(lan: lanURL))

        probe.setReachable([])
        await client.networkPathChanged(cellular)
        try await accept(away, on: client, welcome: welcome(lan: lanURL))
        #expect(factory.connections.map(\.url) == [lanURL, publicURL])
        await client.stop()
    }

    @Test func unchangedRouteKeepsTheConnection() async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport, FakeTransport()], bridge: bridge)
        let probe = StubProbe()
        let client = makeClient(factory, probe: probe, lan: lanURL)
        await client.networkPathChanged(cellular)
        await client.start()
        try await accept(transport, on: client, welcome: welcome(lan: lanURL))

        // Wi-Fi without the bridge (e.g. a café): still public.
        await client.networkPathChanged(wifi)
        try await Task.sleep(for: .milliseconds(100))
        #expect(factory.connectionCount == 1)
        #expect(await client.state.welcome != nil)
        await client.stop()
    }

    @Test func ignoresInvalidLanURLFromWelcome() async throws {
        let transport = FakeTransport()
        let factory = FakeFactory(transports: [transport], bridge: bridge)
        let client = makeClient(factory, probe: StubProbe(), lan: nil)
        await client.start()
        try await accept(transport, on: client, welcome: welcome(lan: URL(string: "https://evil.example.com/")!))
        #expect(await client.lanURL == nil)
        await client.stop()
    }
}

struct PairingLinkLanTests {
    let fp = "If4x36FUomFia_hUBG_SJxt77UtqvkWqWId-9H-XIbk"

    @Test func parsesLan() throws {
        let link = try PairingLink(string: "housephone://pair?v=2&url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws&lan=ws%3A%2F%2F192.168.178.20%3A8081%2Fv1%2Fws&code=K7P2XH9QRMW4DZT8&fp=\(fp)&name=Zuhause")
        #expect(link.lanURL == URL(string: "ws://192.168.178.20:8081/v1/ws"))
        #expect(link.pairingURL == link.lanURL)
        #expect(link.bridgeURL == URL(string: "wss://phone.example.com/v1/ws"))
    }

    @Test func linkWithoutLanPairsOverURL() throws {
        let link = try PairingLink(string: "housephone://pair?v=2&url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws&code=K7P2XH9QRMW4DZT8&fp=\(fp)")
        #expect(link.lanURL == nil)
        #expect(link.pairingURL == link.bridgeURL)
    }

    @Test func rejectsInvalidLan() {
        #expect(throws: PairingLinkError.invalidLanURL) {
            try PairingLink(string: "housephone://pair?v=2&url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws&lan=https%3A%2F%2Fx&code=K7P2XH9QRMW4DZT8&fp=\(fp)")
        }
    }

    @Test func companionInstructionCarriesLan() throws {
        let pairing = CompanionPairing(
            code: "K7P2XH9QRMW4DZT8", url: URL(string: "wss://phone.example.com/v1/ws")!,
            lanUrl: URL(string: "ws://192.168.178.20:8081/v1/ws")!, expiresAt: Date(timeIntervalSince1970: 1_800_000_000)
        )
        let instruction = CompanionPairingInstruction(pairing: pairing, bridgeName: "Zuhause", fingerprint: fp)
        let decoded = try #require(CompanionPairingInstruction(dictionary: instruction.dictionary))
        #expect(decoded.link.lanURL == pairing.lanUrl)
        #expect(decoded.link.pairingURL == pairing.lanUrl)
    }
}
