import AVFAudio
import HousephoneKit
import os
import WebRTC

enum MediaEngineError: Error {
    case peerConnectionUnavailable
    case missingDescription
}

/// Owns the WebRTC peer connection of the current call. The bridge is
/// always the offerer; this side only answers.
///
/// Audio follows CallKit: WebRTC's audio unit only runs between
/// `provider(_:didActivate:)` and `provider(_:didDeactivate:)`.
@MainActor
final class MediaEngine {
    enum ConnectionState: Equatable {
        case connecting
        case connected
        case interrupted
        case failed
    }

    /// Called when the media path changes state, e.g. to show
    /// "Verbindung wird wiederhergestellt" on the call screen.
    var onConnectionStateChange: ((CallID, ConnectionState) -> Void)?

    private let factory: RTCPeerConnectionFactory
    private let audioSession = RTCAudioSession.sharedInstance()
    private var peer: Peer?
    private var gatheringWaiters: [CheckedContinuation<Void, Never>] = []
    private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "media")

    private struct Peer {
        let callId: CallID
        let connection: RTCPeerConnection
        let observer: PeerObserver
        var localTrack: RTCAudioTrack?
    }

    init() {
        RTCInitializeSSL()
        factory = RTCPeerConnectionFactory()
        audioSession.useManualAudio = true
        audioSession.isAudioEnabled = false
    }

    // MARK: - Audio session (driven by CallKit)

    /// Sets the category before CallKit activates the session.
    func configureAudioSession() {
        let configuration = RTCAudioSessionConfiguration.webRTC()
        configuration.category = AVAudioSession.Category.playAndRecord.rawValue
        configuration.mode = AVAudioSession.Mode.voiceChat.rawValue
        configuration.categoryOptions = [.allowBluetoothHFP]
        audioSession.lockForConfiguration()
        defer { audioSession.unlockForConfiguration() }
        do {
            try audioSession.setConfiguration(configuration)
        } catch {
            logger.error("Audio session configuration failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    func audioSessionDidActivate(_ session: AVAudioSession) {
        audioSession.audioSessionDidActivate(session)
        audioSession.isAudioEnabled = true
    }

    func audioSessionDidDeactivate(_ session: AVAudioSession) {
        audioSession.audioSessionDidDeactivate(session)
        audioSession.isAudioEnabled = false
    }

    // MARK: - Negotiation

    /// Applies the bridge's offer and returns the complete answer SDP
    /// (ICE gathering finished or 2 s passed — no trickle ICE in v1).
    /// A second offer for the same call is an ICE restart.
    func answer(_ offer: SessionOffer) async throws -> String {
        let peer = try peer(for: offer)
        try await setRemoteDescription(on: peer.connection, sdp: offer.sdp)
        attachLocalAudio()
        let answer = try await createAnswer(on: peer.connection)
        try await setLocalDescription(on: peer.connection, sdp: answer)
        await waitForIceGathering(on: peer.connection, timeout: .seconds(2))
        guard let sdp = peer.connection.localDescription?.sdp else { throw MediaEngineError.missingDescription }
        return sdp
    }

    func setMuted(_ muted: Bool) {
        peer?.localTrack?.isEnabled = !muted
    }

    func close(callId: CallID) {
        guard let peer, peer.callId == callId else { return }
        peer.connection.close()
        self.peer = nil
        resumeGatheringWaiters()
    }

    // MARK: - Private

    private func peer(for offer: SessionOffer) throws -> Peer {
        if let peer, peer.callId == offer.callId { return peer }
        if let peer { close(callId: peer.callId) }

        let configuration = RTCConfiguration()
        configuration.sdpSemantics = .unifiedPlan
        configuration.bundlePolicy = .maxBundle
        configuration.rtcpMuxPolicy = .require
        configuration.continualGatheringPolicy = .gatherOnce
        configuration.iceServers = offer.iceServers.map {
            RTCIceServer(urlStrings: $0.urls, username: $0.username, credential: $0.credential)
        }

        let callId = offer.callId
        let observer = PeerObserver { [weak self] event in
            Task { @MainActor in self?.handle(event, for: callId) }
        }
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        guard let connection = factory.peerConnection(with: configuration, constraints: constraints, delegate: observer) else {
            throw MediaEngineError.peerConnectionUnavailable
        }
        let peer = Peer(callId: callId, connection: connection, observer: observer)
        self.peer = peer
        return peer
    }

    private func attachLocalAudio() {
        guard var peer, peer.localTrack == nil else { return }
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        let source = factory.audioSource(with: constraints)
        let track = factory.audioTrack(with: source, trackId: "housephone-audio")
        peer.connection.add(track, streamIds: ["housephone"])
        peer.localTrack = track
        self.peer = peer
    }

    private func handle(_ event: PeerObserver.Event, for callId: CallID) {
        guard peer?.callId == callId else { return }
        switch event {
        case .gatheringComplete:
            resumeGatheringWaiters()
        case .iceConnection(let state):
            logger.info("ICE \(state.rawValue)")
            let mapped: ConnectionState? = switch state {
            case .checking, .new: .connecting
            case .connected, .completed: .connected
            case .disconnected: .interrupted
            case .failed: .failed
            default: nil
            }
            if let mapped { onConnectionStateChange?(callId, mapped) }
        }
    }

    private func waitForIceGathering(on connection: RTCPeerConnection, timeout: Duration) async {
        guard connection.iceGatheringState != .complete else { return }
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            gatheringWaiters.append(continuation)
            Task {
                try? await Task.sleep(for: timeout)
                self.resumeGatheringWaiters()
            }
        }
    }

    private func resumeGatheringWaiters() {
        let waiters = gatheringWaiters
        gatheringWaiters.removeAll()
        for waiter in waiters { waiter.resume() }
    }

    private func setRemoteDescription(on connection: RTCPeerConnection, sdp: String) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            connection.setRemoteDescription(RTCSessionDescription(type: .offer, sdp: sdp)) { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            }
        }
    }

    private func createAnswer(on connection: RTCPeerConnection) async throws -> String {
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, any Error>) in
            connection.answer(for: constraints) { description, error in
                if let error {
                    continuation.resume(throwing: error)
                } else if let sdp = description?.sdp {
                    continuation.resume(returning: sdp)
                } else {
                    continuation.resume(throwing: MediaEngineError.missingDescription)
                }
            }
        }
    }

    private func setLocalDescription(on connection: RTCPeerConnection, sdp: String) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            connection.setLocalDescription(RTCSessionDescription(type: .answer, sdp: sdp)) { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            }
        }
    }
}

/// Receives WebRTC callbacks on its signaling thread and forwards the few
/// the app needs.
final class PeerObserver: NSObject, RTCPeerConnectionDelegate, @unchecked Sendable {
    enum Event: Sendable {
        case gatheringComplete
        case iceConnection(RTCIceConnectionState)
    }

    private let onEvent: @Sendable (Event) -> Void

    init(onEvent: @escaping @Sendable (Event) -> Void) {
        self.onEvent = onEvent
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {
        if newState == .complete { onEvent(.gatheringComplete) }
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {
        onEvent(.iceConnection(newState))
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}
}
