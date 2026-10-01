import Foundation
import HousephoneKit
import Observation
import os
import WatchKit

/// The watch's relation to the bridge: credentials, push-token
/// registration over HTTPS, and signaling clients for calls.
///
/// watchOS allows WebSocket only while a CallKit call runs (TN3135), so
/// everything outside a call — pairing, registering the push token — goes
/// over plain HTTPS.
@MainActor
@Observable
final class WatchBridge {
    enum Registration: Equatable {
        case none
        case registering
        case registered
        case failed
    }

    private(set) var credentials: BridgeCredentials?
    private(set) var pushToken: String?
    private(set) var registration: Registration = .none
    private(set) var isPairing = false
    /// The bridge no longer knows this watch (401). Only pairing again helps.
    private(set) var isRejected = false
    /// Why the last pairing attempt failed, for the watch's own screen.
    private(set) var lastFailure: String?

    /// Called whenever the state the iPhone should see changes.
    @ObservationIgnored var onStateChange: ((WatchPairingState) -> Void)?
    /// Called when the watch gets paired or unpaired, e.g. to switch VoIP
    /// pushes on or off.
    @ObservationIgnored var onPairedChange: ((Bool) -> Void)?

    @ObservationIgnored private let store: any CredentialStore
    @ObservationIgnored private let keyStore: any DeviceKeyStore
    @ObservationIgnored private let http: BridgeHTTPClient
    @ObservationIgnored private var retryTask: Task<Void, Never>?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone.watch", category: "bridge")

    /// The watch app's bundle ID plus `.voip` (see BridgeConnection).
    static let voipPushTopic = (Bundle.main.bundleIdentifier ?? "com.jorisconrad.housephone.watchkitapp") + ".voip"
    private static let pairedAtKey = "housephone.pairedAt"

    /// When this watch paired, to ignore an unpair instruction from the
    /// iPhone that was queued before a newer pairing.
    private var pairedAt: Date? {
        get {
            let seconds = UserDefaults.standard.double(forKey: Self.pairedAtKey)
            return seconds > 0 ? Date(timeIntervalSince1970: seconds) : nil
        }
        set {
            if let newValue {
                UserDefaults.standard.set(newValue.timeIntervalSince1970, forKey: Self.pairedAtKey)
            } else {
                UserDefaults.standard.removeObject(forKey: Self.pairedAtKey)
            }
        }
    }

    /// The watch has its own device key (Secure Enclave), independent of
    /// the iPhone's; `keyStore` holds it.
    init(store: any CredentialStore, keyStore: any DeviceKeyStore = KeychainDeviceKeyStore(), http: BridgeHTTPClient? = nil) {
        self.store = store
        self.keyStore = keyStore
        self.http = http ?? BridgeHTTPClient(keyStore: keyStore)
        credentials = try? store.load()
    }

    var isPaired: Bool { credentials != nil && !isRejected }
    var bridgeName: String? { credentials?.bridgeName }

    /// Incoming calls can ring: paired and the push token is at the bridge.
    var canReceiveCalls: Bool { isPaired && registration == .registered }

    var pairingState: WatchPairingState {
        if isPairing { return WatchPairingState(phase: .pairing, bridgeName: bridgeName) }
        guard isPaired else { return WatchPairingState(phase: .unpaired) }
        return WatchPairingState(phase: .paired, bridgeName: bridgeName, canReceiveCalls: canReceiveCalls)
    }

    // MARK: - Pairing

    /// Pairs with the code the iPhone handed over. Returns the resulting
    /// state, which also goes to `onStateChange`.
    func pair(_ instruction: CompanionPairingInstruction) async -> WatchPairingState {
        guard !instruction.isExpired() else {
            return failed(String(localized: "Der Code ist abgelaufen. Starte die Kopplung auf dem iPhone neu."))
        }
        isPairing = true
        publish()
        defer { isPairing = false }

        do {
            let paired = try await http.pair(
                link: instruction.link,
                deviceName: WKInterfaceDevice.current().name,
                platform: .watchos,
                model: Self.hardwareModel
            )
            do {
                try store.save(paired)
            } catch {
                try? keyStore.deleteKey(tag: paired.keyTag)
                throw error
            }
            if let previousKeyTag = credentials?.keyTag, previousKeyTag != paired.keyTag {
                try? keyStore.deleteKey(tag: previousKeyTag)
            }
            credentials = paired
            pairedAt = .now
            isRejected = false
            lastFailure = nil
            registration = .none
            logger.info("Paired with \(paired.bridgeName, privacy: .public)")
            onPairedChange?(true)
        } catch {
            logger.error("Pairing failed: \(String(describing: error), privacy: .public)")
            return failed(Self.message(for: error))
        }

        await registerDevice()
        let state = pairingState
        onStateChange?(state)
        return state
    }

    /// Tells the bridge (best effort) and forgets the credentials. The
    /// watch stops receiving VoIP pushes.
    func unpair() async {
        retryTask?.cancel()
        if let credentials {
            do {
                try await http.deleteDevice(credentials: credentials)
            } catch {
                // Offline or already removed: the bridge drops the push token
                // once APNs reports it as unregistered.
                logger.info("Bridge not told about unpairing: \(String(describing: error), privacy: .public)")
            }
        }
        try? store.delete()
        if let keyTag = credentials?.keyTag {
            try? keyStore.deleteKey(tag: keyTag)
        }
        credentials = nil
        pairedAt = nil
        registration = .none
        isRejected = false
        lastFailure = nil
        logger.info("Unpaired")
        onPairedChange?(false)
        publish()
    }

    /// The iPhone was unpaired. Follows unless the watch paired again after
    /// the iPhone sent this (a late delivery from the transfer queue).
    func unpair(following instruction: CompanionUnpairInstruction) async {
        guard credentials != nil else {
            publish()
            return
        }
        guard instruction.applies(toPairingAt: pairedAt) else {
            logger.info("Ignoring unpair instruction older than the current pairing")
            publish()
            return
        }
        await unpair()
    }

    // MARK: - Push token

    func updatePushToken(_ token: String?) {
        guard token != pushToken else { return }
        pushToken = token
        registration = .none
        Task { await registerDevice() }
    }

    /// Tells the bridge this watch's push token, capabilities and topic.
    /// Retries with backoff, since the watch may be offline for a while.
    func registerDevice(attempt: Int = 0) async {
        retryTask?.cancel()
        guard let credentials, let pushToken, !isRejected else {
            publish()
            return
        }
        registration = .registering
        let update = DeviceUpdate(
            pushToken: pushToken,
            pushEnvironment: Self.pushEnvironment,
            mediaCapabilities: [.webSocketPCMA],
            pushTopic: Self.voipPushTopic
        )
        do {
            try await http.updateDevice(update, credentials: credentials)
            registration = .registered
        } catch SignalingClientError.unauthorized {
            logger.error("Bridge rejected this watch")
            registration = .failed
            isRejected = true
        } catch {
            logger.info("Registering push token failed: \(String(describing: error), privacy: .public)")
            registration = .failed
            let delay = Duration.seconds(min(300, 5 << min(attempt, 6)))
            retryTask = Task { [weak self] in
                try? await Task.sleep(for: delay)
                guard !Task.isCancelled else { return }
                await self?.registerDevice(attempt: attempt + 1)
            }
        }
        publish()
    }

    // MARK: - Calls

    /// A signaling client for one call. The caller starts and stops it.
    func makeCallClient() -> SignalingClient? {
        guard let credentials, !isRejected else { return nil }
        let hello = Hello(
            appVersion: Self.appVersion,
            platform: .watchos,
            pushToken: pushToken,
            pushEnvironment: pushToken == nil ? nil : Self.pushEnvironment,
            mediaCapabilities: [.webSocketPCMA],
            pushTopic: Self.voipPushTopic
        )
        var configuration = SignalingClient.Configuration()
        configuration.initialBackoff = .milliseconds(300)
        configuration.maximumBackoff = .seconds(3)
        return SignalingClient(credentials: credentials, hello: hello, keyStore: keyStore, configuration: configuration)
    }

    /// The call as the bridge sees it for this watch (`GET /v1/calls/{id}`),
    /// or nil if the bridge can't be asked right now.
    func callStatus(_ callId: CallID) async -> BridgeCallStatus? {
        guard let credentials, !isRejected else { return nil }
        do {
            return try await http.callStatus(callId, credentials: credentials)
        } catch SignalingClientError.unauthorized {
            markRejected()
            return nil
        } catch {
            logger.info("Call status unavailable: \(String(describing: error), privacy: .public)")
            return nil
        }
    }

    /// The bridge answered 401 on a call connection.
    func markRejected() {
        isRejected = true
        registration = .failed
        publish()
    }

    // MARK: - Private

    private func failed(_ message: String) -> WatchPairingState {
        lastFailure = message
        let state = WatchPairingState(phase: .failed, bridgeName: bridgeName, failure: message)
        onStateChange?(state)
        return state
    }

    private func publish() {
        onStateChange?(pairingState)
    }

    private static func message(for error: any Error) -> String {
        switch error {
        case SignalingClientError.bridge(let payload) where payload.code == .pairingInvalid:
            String(localized: "Der Code ist ungültig oder abgelaufen. Starte die Kopplung auf dem iPhone neu.")
        case SignalingClientError.bridge(let payload) where payload.code == .pairingRateLimited:
            String(localized: "Zu viele Versuche. Warte eine Minute und versuche es dann erneut.")
        case HP2Error.bridgeIdentityMismatch:
            String(localized: "Diese Bridge ist nicht die, mit der das iPhone gekoppelt ist.")
        case BridgeHTTPError.homeNetworkRequired:
            String(localized: "Zum Koppeln ins Heim-WLAN oder Tailscale")
        case let error as URLError where error.code == .notConnectedToInternet || error.code == .networkConnectionLost:
            String(localized: "Die Watch ist offline. Verbinde sie mit dem iPhone oder einem WLAN.")
        case is URLError:
            String(localized: "Die Bridge ist von der Watch aus nicht erreichbar.")
        default:
            String(localized: "Die Kopplung ist fehlgeschlagen.")
        }
    }

    static var appVersion: String {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "0"
        let build = info?["CFBundleVersion"] as? String ?? "0"
        return "\(version) (\(build))"
    }

    static var pushEnvironment: PushEnvironment {
        #if DEBUG
        .development
        #else
        .production
        #endif
    }

    /// e.g. `Watch7,1`.
    static var hardwareModel: String {
        var systemInfo = utsname()
        uname(&systemInfo)
        return withUnsafeBytes(of: &systemInfo.machine) { buffer in
            String(decoding: buffer.prefix { $0 != 0 }, as: UTF8.self)
        }
    }
}
