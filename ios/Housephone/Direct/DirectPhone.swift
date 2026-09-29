import Foundation
import HousephoneKit
import Network
import Observation
import os

/// The mode without bridge (ADR-0005): this iPhone registers at the
/// FRITZ!Box as an IP phone and carries calls itself — only on the home
/// Wi-Fi. CallKit stays in `CallCenter`; this class owns the SIP user
/// agent and the RTP audio.
@MainActor
@Observable
final class DirectPhone {
    enum Status: Equatable {
        /// Not set up; the app runs with a bridge (or not at all yet).
        case off
        /// No Wi-Fi, or the FRITZ!Box does not answer: away from home.
        case notAtHome
        case connecting
        case ready
        case wrongPassword
        case rejected(Int)
    }

    private(set) var configuration: DirectConfiguration?
    private(set) var status: Status = .off

    var isEnabled: Bool { configuration != nil }
    var canCall: Bool { status == .ready }

    /// Call events of the user agent, for `CallCenter`.
    @ObservationIgnored var onCallEvent: ((SIPEvent) -> Void)?
    @ObservationIgnored let media = DirectMedia()

    @ObservationIgnored private let store = DirectConfigurationStore()
    @ObservationIgnored private var agent: SIPUserAgent?
    @ObservationIgnored private var eventTask: Task<Void, Never>?
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private let pathMonitor = NWPathMonitor(requiredInterfaceType: .wifi)
    @ObservationIgnored private var isOnWiFi = false
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "direct")

    init() {
        configuration = store.load()
        status = configuration == nil ? .off : .notAtHome
        pathMonitor.pathUpdateHandler = { [weak self] path in
            let onWiFi = path.status == .satisfied
            Task { @MainActor in self?.wifiChanged(onWiFi) }
        }
        pathMonitor.start(queue: DispatchQueue(label: "com.jorisconrad.housephone.direct.path"))
    }

    // MARK: - Setup

    func configure(_ configuration: DirectConfiguration) throws {
        try store.save(configuration)
        self.configuration = configuration
        restart()
    }

    /// Back to "no mode": unregisters and deletes the stored access data.
    func disable() {
        let agent = agent
        tearDown()
        Task { await agent?.stop() }
        store.delete()
        configuration = nil
        status = .off
    }

    /// E.g. when the app comes to the foreground: register again right
    /// away instead of waiting for the next retry.
    func refresh() {
        guard isEnabled else { return }
        switch status {
        case .ready, .connecting:
            let agent = agent
            Task { await agent?.reregister() }
        case .notAtHome, .rejected, .off:
            restart()
        case .wrongPassword:
            // Retrying a wrong password only gets the FRITZ!Box to block.
            break
        }
    }

    private func wifiChanged(_ onWiFi: Bool) {
        guard onWiFi != isOnWiFi else { return }
        isOnWiFi = onWiFi
        logger.info("Wi-Fi \(onWiFi ? "available" : "gone", privacy: .public)")
        restart()
    }

    private func restart() {
        let old = agent
        tearDown()
        Task { await old?.stop() }
        guard let configuration else {
            status = .off
            return
        }
        guard isOnWiFi else {
            status = .notAtHome
            return
        }
        status = .connecting
        let account = configuration.account
        let transport = UDPSIPTransport(host: account.registrar, port: account.port)
        let agent = SIPUserAgent(account: account, transport: transport)
        self.agent = agent
        let generation = generation
        eventTask = Task { [weak self] in
            for await event in agent.events {
                guard let self, self.generation == generation else { return }
                self.handle(event)
            }
        }
        Task { await agent.start() }
    }

    private func tearDown() {
        generation += 1
        eventTask?.cancel()
        eventTask = nil
        agent = nil
    }

    private func handle(_ event: SIPEvent) {
        switch event {
        case .registration(let state):
            switch state {
            case .registered: status = .ready
            case .registering: status = .connecting
            case .failed(.authentication): status = .wrongPassword
            case .failed(.rejected(let code)): status = .rejected(code)
            case .failed(.timeout), .failed(.transport): status = .notAtHome
            case .unregistered: break
            }
            logger.info("Registration: \(String(describing: state), privacy: .public)")
        default:
            onCallEvent?(event)
        }
    }

    // MARK: - Calls

    func call(_ number: String) async -> String? {
        guard let agent else { return nil }
        return try? await agent.call(number)
    }

    func answer(_ id: String) async -> Bool {
        await agent?.answer(id) ?? false
    }

    func hangUp(_ id: String) {
        let agent = agent
        Task { await agent?.hangUp(id) }
    }

    func reject(_ id: String, busy: Bool) {
        let agent = agent
        Task { await agent?.reject(id, busy: busy) }
    }
}

/// RTP and audio of the running direct call. The audio engine runs
/// between CallKit's `didActivate` and `didDeactivate`; the RTP flow from
/// the first remote SDP (early media or answer) until the call ends.
final class DirectMedia: @unchecked Sendable {
    let audio = CallAudio(framing: .raw, logSubsystem: "com.jorisconrad.housephone")

    private let lock = NSLock()
    private var session: RTPSession?
    private var remote: SDPMedia?
    private var sender: RTPSender?
    private var pendingDigits: [Character] = []
    private var digitPackets: [Data] = []
    /// Audio frames to send between two digits.
    private var pauseFrames = 0
    private var receiveTask: Task<Void, Never>?

    init() {
        audio.onFrame = { [weak self] payload in self?.send(payload) }
    }

    /// Starts RTP towards `media` from `localPort`; a second call with the
    /// same remote side (early media, then the answer) keeps the flow.
    func connect(to media: SDPMedia, localPort: UInt16) {
        lock.lock()
        defer { lock.unlock() }
        if let remote, remote.address == media.address, remote.port == media.port { return }
        receiveTask?.cancel()
        session?.close()
        let session = RTPSession(localPort: localPort, remoteHost: media.address, remotePort: media.port)
        self.session = session
        remote = media
        sender = RTPSender(payloadType: media.codec.rawValue, telephoneEventPayloadType: media.telephoneEventPayloadType)
        audio.resetPlayout()
        let audio = audio
        let payloadType = media.codec.rawValue
        receiveTask = Task.detached {
            for await packet in session.incoming where packet.payloadType == payloadType {
                audio.receive(packet.payload)
            }
        }
    }

    /// Queues RFC 4733 events; they replace audio frames, one per 20 ms.
    /// Returns false when the remote side took no telephone events.
    @discardableResult
    func sendDTMF(_ digits: String) -> Bool {
        lock.withLock {
            guard sender?.telephoneEventPayloadType != nil else { return false }
            pendingDigits += digits
            return true
        }
    }

    func close() {
        lock.lock()
        receiveTask?.cancel()
        receiveTask = nil
        session?.close()
        session = nil
        remote = nil
        sender = nil
        pendingDigits = []
        digitPackets = []
        pauseFrames = 0
        lock.unlock()
        audio.setRingback(false)
    }

    private func send(_ payload: Data) {
        lock.lock()
        guard let session, var sender else {
            lock.unlock()
            return
        }
        // Digits are packetized when their turn comes, so sequence numbers
        // and timestamps stay in order with the audio around them.
        if digitPackets.isEmpty, pauseFrames == 0, !pendingDigits.isEmpty {
            digitPackets = sender.dtmf(pendingDigits.removeFirst()) ?? []
        }
        let datagram: Data
        if !digitPackets.isEmpty {
            datagram = digitPackets.removeFirst()
            if digitPackets.isEmpty { pauseFrames = 3 }
        } else {
            pauseFrames = max(pauseFrames - 1, 0)
            datagram = sender.audio(payload)
        }
        self.sender = sender
        lock.unlock()
        session.send(datagram)
    }
}
