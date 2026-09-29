import Foundation

// Wire format of `docs/protocol/signaling-v1.md` (calls) and
// `signaling-v2.md` (pairing, authentication). Every message is
// `{"type": "...", "payload": {...}}`; unknown types decode to `.unknown`
// and unknown fields are ignored.

public enum DevicePlatform: String, Codable, Sendable {
    case ios
    case watchos
}

public enum PushEnvironment: String, Codable, Sendable {
    case development
    case production
}

/// How a device can carry call audio (signaling v1.1). Unknown values are
/// kept verbatim so newer bridges and devices stay compatible.
public struct MediaCapability: RawRepresentable, Codable, Sendable, Hashable {
    public var rawValue: String

    public init(rawValue: String) {
        self.rawValue = rawValue
    }

    public init(from decoder: any Decoder) throws {
        rawValue = try decoder.singleValueContainer().decode(String.self)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }

    /// WebRTC with `call.offer`/`call.answer` (iPhone).
    public static let webRTC = Self(rawValue: "webrtc")
    /// A-law frames as binary WebSocket messages after `call.media` (Watch).
    public static let webSocketPCMA = Self(rawValue: "websocket-pcma")
}

// MARK: - Device → bridge

/// Body of `POST /v1/pair` (signaling v2). `publicKey` is the device's
/// P-256 key (X9.63, base64url), `proof` its signature over the code and
/// `nonce` (see `HP2.pairProofMessage`).
public struct PairRequest: Codable, Sendable, Equatable {
    public var code: String
    public var deviceName: String
    public var platform: DevicePlatform
    public var model: String?
    public var publicKey: String
    public var nonce: String
    public var proof: String

    public init(code: String, deviceName: String, platform: DevicePlatform, model: String? = nil, publicKey: String, nonce: String, proof: String) {
        self.code = code
        self.deviceName = deviceName
        self.platform = platform
        self.model = model
        self.publicKey = publicKey
        self.nonce = nonce
        self.proof = proof
    }
}

public struct Hello: Codable, Sendable, Equatable {
    public var appVersion: String
    public var platform: DevicePlatform
    public var pushToken: String?
    public var pushEnvironment: PushEnvironment?
    /// Missing means `["webrtc"]` (v1 devices).
    public var mediaCapabilities: [MediaCapability]?
    /// APNs topic for this device's VoIP pushes. Missing means the bridge's default.
    public var pushTopic: String?

    public init(
        appVersion: String,
        platform: DevicePlatform,
        pushToken: String? = nil,
        pushEnvironment: PushEnvironment? = nil,
        mediaCapabilities: [MediaCapability]? = nil,
        pushTopic: String? = nil
    ) {
        self.appVersion = appVersion
        self.platform = platform
        self.pushToken = pushToken
        self.pushEnvironment = pushEnvironment
        self.mediaCapabilities = mediaCapabilities
        self.pushTopic = pushTopic
    }
}

public struct DeviceUpdate: Codable, Sendable, Equatable {
    public var pushToken: String?
    public var pushEnvironment: PushEnvironment?
    public var deviceName: String?
    public var mediaCapabilities: [MediaCapability]?
    public var pushTopic: String?

    public init(
        pushToken: String? = nil,
        pushEnvironment: PushEnvironment? = nil,
        deviceName: String? = nil,
        mediaCapabilities: [MediaCapability]? = nil,
        pushTopic: String? = nil
    ) {
        self.pushToken = pushToken
        self.pushEnvironment = pushEnvironment
        self.deviceName = deviceName
        self.mediaCapabilities = mediaCapabilities
        self.pushTopic = pushTopic
    }
}

/// `pair.companion.request`: a paired device asks for a pairing code for
/// another device, e.g. the iPhone for its Apple Watch.
public struct CompanionPairingRequest: Codable, Sendable, Equatable {
    public var deviceName: String
    public var platform: DevicePlatform

    public init(deviceName: String, platform: DevicePlatform) {
        self.deviceName = deviceName
        self.platform = platform
    }
}

public struct CallReference: Codable, Sendable, Equatable {
    public var callId: CallID

    public init(callId: CallID) {
        self.callId = callId
    }
}

public struct DialRequest: Codable, Sendable, Equatable {
    public var callId: CallID
    public var number: String

    public init(callId: CallID, number: String) {
        self.callId = callId
        self.number = number
    }
}

public struct SessionAnswer: Codable, Sendable, Equatable {
    public var callId: CallID
    public var sdp: String

    public init(callId: CallID, sdp: String) {
        self.callId = callId
        self.sdp = sdp
    }
}

public enum HangupReason: String, Codable, Sendable {
    case hangup
    case declined
    case failed
}

public struct Hangup: Codable, Sendable, Equatable {
    public var callId: CallID
    public var reason: HangupReason?

    public init(callId: CallID, reason: HangupReason? = nil) {
        self.callId = callId
        self.reason = reason
    }
}

public struct DTMFDigits: Codable, Sendable, Equatable {
    public var callId: CallID
    public var digits: String

    public init(callId: CallID, digits: String) {
        self.callId = callId
        self.digits = digits
    }
}

// MARK: - Bridge → device

/// Response of `POST /v1/pair` (signaling v2). `signature` is the bridge's
/// Ed25519 signature over the pairing (see `HP2.pairResponseMessage`).
public struct PairingResult: Codable, Sendable, Equatable {
    public var deviceId: DeviceID
    public var bridgeId: String
    public var bridgeName: String
    /// Ed25519 public key (32 bytes), base64url.
    public var bridgePublicKey: String
    public var signature: String

    public init(deviceId: DeviceID, bridgeId: String, bridgeName: String, bridgePublicKey: String, signature: String) {
        self.deviceId = deviceId
        self.bridgeId = bridgeId
        self.bridgeName = bridgeName
        self.bridgePublicKey = bridgePublicKey
        self.signature = signature
    }
}

/// `device.paired` (v2): another device was just paired with the bridge.
public struct DevicePaired: Codable, Sendable, Equatable {
    public var deviceName: String
    public var platform: DevicePlatform
    public var pairedAt: Date

    public init(deviceName: String, platform: DevicePlatform, pairedAt: Date) {
        self.deviceName = deviceName
        self.platform = platform
        self.pairedAt = pairedAt
    }
}

public struct Welcome: Codable, Sendable, Equatable {
    public var bridgeId: String
    public var bridgeName: String
    public var bridgeVersion: String
    public var sipRegistered: Bool
    /// Extra functions (v1.2), e.g. the FRITZ!Box phonebook. `nil` from
    /// bridges before v1.2.
    public var features: [BridgeFeature]?
    /// The bridge's private listener (home network / Tailscale), preferred
    /// when reachable. `nil` from bridges without one.
    public var lanUrl: URL?

    public init(bridgeId: String, bridgeName: String, bridgeVersion: String, sipRegistered: Bool, features: [BridgeFeature]? = nil, lanUrl: URL? = nil) {
        self.bridgeId = bridgeId
        self.bridgeName = bridgeName
        self.bridgeVersion = bridgeVersion
        self.sipRegistered = sipRegistered
        self.features = features
        self.lanUrl = lanUrl
    }

    public func supports(_ feature: BridgeFeature) -> Bool {
        features?.contains(feature) == true
    }
}

/// `pair.companion`: a fresh one-time pairing code for a companion device.
public struct CompanionPairing: Codable, Sendable, Equatable {
    public var code: String
    public var url: URL
    /// Where the companion pairs (private listener, home network).
    public var lanUrl: URL?
    public var expiresAt: Date

    public init(code: String, url: URL, lanUrl: URL? = nil, expiresAt: Date) {
        self.code = code
        self.url = url
        self.lanUrl = lanUrl
        self.expiresAt = expiresAt
    }
}

/// `call.media`: the call's audio runs as binary WebSocket frames.
public struct CallMedia: Codable, Sendable, Equatable {
    public var callId: CallID
    public var transport: String
    public var codec: String
    public var sampleRate: Int
    public var frameMs: Int

    public init(callId: CallID, transport: String = "websocket", codec: String = "PCMA", sampleRate: Int = 8000, frameMs: Int = 20) {
        self.callId = callId
        self.transport = transport
        self.codec = codec
        self.sampleRate = sampleRate
        self.frameMs = frameMs
    }

    /// Whether this device can play and record the offered format.
    public var isSupported: Bool {
        transport == "websocket" && codec.uppercased() == "PCMA" && sampleRate == 8000 && frameMs == 20
    }
}

public struct BridgeStatus: Codable, Sendable, Equatable {
    public var sipRegistered: Bool

    public init(sipRegistered: Bool) {
        self.sipRegistered = sipRegistered
    }
}

public struct IncomingCall: Codable, Sendable, Equatable {
    public var callId: CallID
    public var caller: String
    public var callerName: String?
    public var startedAt: Date

    public init(callId: CallID, caller: String, callerName: String? = nil, startedAt: Date) {
        self.callId = callId
        self.caller = caller
        self.callerName = callerName
        self.startedAt = startedAt
    }
}

public struct ICEServer: Codable, Sendable, Equatable {
    public var urls: [String]
    public var username: String?
    public var credential: String?

    public init(urls: [String], username: String? = nil, credential: String? = nil) {
        self.urls = urls
        self.username = username
        self.credential = credential
    }
}

public struct SessionOffer: Codable, Sendable, Equatable {
    public var callId: CallID
    public var sdp: String
    public var iceServers: [ICEServer]

    public init(callId: CallID, sdp: String, iceServers: [ICEServer] = []) {
        self.callId = callId
        self.sdp = sdp
        self.iceServers = iceServers
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        callId = try container.decode(CallID.self, forKey: .callId)
        sdp = try container.decode(String.self, forKey: .sdp)
        // The spec allows an empty list; tolerate a missing one as well.
        iceServers = try container.decodeIfPresent([ICEServer].self, forKey: .iceServers) ?? []
    }
}

public enum RemoteCallState: String, Codable, Sendable {
    case ringing
    case earlyMedia = "early_media"
    case connected
}

public struct CallStateChange: Codable, Sendable, Equatable {
    public var callId: CallID
    public var state: RemoteCallState

    public init(callId: CallID, state: RemoteCallState) {
        self.callId = callId
        self.state = state
    }
}

/// Why the bridge ended a call for this device.
public enum CallEndReason: Codable, Sendable, Equatable, Hashable {
    case remoteHangup
    case remoteCancelled
    case answeredElsewhere
    case declinedElsewhere
    case busy
    case rejected
    case notFound
    case failed
    case localHangup
    /// A reason this app version does not know yet; treated like `failed`.
    case other(String)

    public init(rawValue: String) {
        switch rawValue {
        case "remote_hangup": self = .remoteHangup
        case "remote_cancelled": self = .remoteCancelled
        case "answered_elsewhere": self = .answeredElsewhere
        case "declined_elsewhere": self = .declinedElsewhere
        case "busy": self = .busy
        case "rejected": self = .rejected
        case "not_found": self = .notFound
        case "failed": self = .failed
        case "local_hangup": self = .localHangup
        default: self = .other(rawValue)
        }
    }

    public var rawValue: String {
        switch self {
        case .remoteHangup: "remote_hangup"
        case .remoteCancelled: "remote_cancelled"
        case .answeredElsewhere: "answered_elsewhere"
        case .declinedElsewhere: "declined_elsewhere"
        case .busy: "busy"
        case .rejected: "rejected"
        case .notFound: "not_found"
        case .failed: "failed"
        case .localHangup: "local_hangup"
        case .other(let value): value
        }
    }

    public init(from decoder: any Decoder) throws {
        self.init(rawValue: try decoder.singleValueContainer().decode(String.self))
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }
}

public struct CallEnded: Codable, Sendable, Equatable {
    public var callId: CallID
    public var reason: CallEndReason
    public var sipCode: Int?

    public init(callId: CallID, reason: CallEndReason, sipCode: Int? = nil) {
        self.callId = callId
        self.reason = reason
        self.sipCode = sipCode
    }
}

/// Error codes of the `error` message. Unknown codes are kept verbatim.
public struct SignalingErrorCode: RawRepresentable, Codable, Sendable, Hashable {
    public var rawValue: String

    public init(rawValue: String) {
        self.rawValue = rawValue
    }

    public static let unauthorized = Self(rawValue: "unauthorized")
    public static let badRequest = Self(rawValue: "bad_request")
    public static let pairingInvalid = Self(rawValue: "pairing_invalid")
    public static let pairingRateLimited = Self(rawValue: "pairing_rate_limited")
    public static let sipUnavailable = Self(rawValue: "sip_unavailable")
    public static let callNotFound = Self(rawValue: "call_not_found")
    public static let invalidNumber = Self(rawValue: "invalid_number")
    /// v1.2: TR-064 not set up at the bridge, or the FRITZ!Box refused or
    /// did not answer.
    public static let fritzboxUnavailable = Self(rawValue: "fritzbox_unavailable")
    /// v2: the request's timestamp is off by more than 60 s.
    public static let clockSkew = Self(rawValue: "clock_skew")
    /// Too many parallel calls for this device or the bridge.
    public static let tooManyCalls = Self(rawValue: "too_many_calls")
    /// Pairing only works over the private listener (home network or
    /// Tailscale), not through the public tunnel.
    public static let homeNetworkRequired = Self(rawValue: "home_network_required")
    public static let `internal` = Self(rawValue: "internal")
}

public struct SignalingErrorPayload: Codable, Sendable, Equatable {
    public var code: SignalingErrorCode
    public var message: String
    public var callId: CallID?

    public init(code: SignalingErrorCode, message: String, callId: CallID? = nil) {
        self.code = code
        self.message = message
        self.callId = callId
    }
}

// MARK: - Envelope

/// WebSocket messages. Pairing is HTTPS-only since v2 (`POST /v1/pair`).
public enum SignalingMessage: Sendable, Equatable {
    // Device → bridge
    case hello(Hello)
    case deviceUpdate(DeviceUpdate)
    /// Removes this device from the bridge. The bridge closes the connection.
    case deviceUnpair
    case pairCompanionRequest(CompanionPairingRequest)
    case callAttach(CallReference)
    case callDial(DialRequest)
    case callAnswer(SessionAnswer)
    case callAccept(CallReference)
    case callHangup(Hangup)
    case callDTMF(DTMFDigits)

    // Bridge → device
    case pairCompanion(CompanionPairing)
    /// v2: another device was paired with the bridge.
    case devicePaired(DevicePaired)
    case welcome(Welcome)
    case status(BridgeStatus)
    case callIncoming(IncomingCall)
    case callOffer(SessionOffer)
    case callMedia(CallMedia)
    case callState(CallStateChange)
    case callEnded(CallEnded)
    case error(SignalingErrorPayload)

    /// A message type this app version does not understand.
    case unknown(type: String)

    public var type: String {
        switch self {
        case .hello: "hello"
        case .deviceUpdate: "device.update"
        case .deviceUnpair: "device.unpair"
        case .pairCompanionRequest: "pair.companion.request"
        case .callAttach: "call.attach"
        case .callDial: "call.dial"
        case .callAnswer: "call.answer"
        case .callAccept: "call.accept"
        case .callHangup: "call.hangup"
        case .callDTMF: "call.dtmf"
        case .pairCompanion: "pair.companion"
        case .devicePaired: "device.paired"
        case .welcome: "welcome"
        case .status: "status"
        case .callIncoming: "call.incoming"
        case .callOffer: "call.offer"
        case .callMedia: "call.media"
        case .callState: "call.state"
        case .callEnded: "call.ended"
        case .error: "error"
        case .unknown(let type): type
        }
    }

    /// The call this message refers to, if any.
    public var callId: CallID? {
        switch self {
        case .callAttach(let payload), .callAccept(let payload): payload.callId
        case .callDial(let payload): payload.callId
        case .callAnswer(let payload): payload.callId
        case .callHangup(let payload): payload.callId
        case .callDTMF(let payload): payload.callId
        case .callIncoming(let payload): payload.callId
        case .callOffer(let payload): payload.callId
        case .callMedia(let payload): payload.callId
        case .callState(let payload): payload.callId
        case .callEnded(let payload): payload.callId
        case .error(let payload): payload.callId
        case .hello, .deviceUpdate, .deviceUnpair, .pairCompanionRequest, .pairCompanion, .devicePaired, .welcome, .status, .unknown: nil
        }
    }
}

extension SignalingMessage: Codable {
    private enum CodingKeys: String, CodingKey {
        case type
        case payload
    }

    private struct EmptyPayload: Codable {}

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let type = try container.decode(String.self, forKey: .type)

        func payload<T: Decodable>(_: T.Type) throws -> T {
            try container.decode(T.self, forKey: .payload)
        }

        switch type {
        case "hello": self = .hello(try payload(Hello.self))
        case "device.update": self = .deviceUpdate(try payload(DeviceUpdate.self))
        case "device.unpair": self = .deviceUnpair
        case "pair.companion.request": self = .pairCompanionRequest(try payload(CompanionPairingRequest.self))
        case "call.attach": self = .callAttach(try payload(CallReference.self))
        case "call.dial": self = .callDial(try payload(DialRequest.self))
        case "call.answer": self = .callAnswer(try payload(SessionAnswer.self))
        case "call.accept": self = .callAccept(try payload(CallReference.self))
        case "call.hangup": self = .callHangup(try payload(Hangup.self))
        case "call.dtmf": self = .callDTMF(try payload(DTMFDigits.self))
        case "pair.companion": self = .pairCompanion(try payload(CompanionPairing.self))
        case "device.paired": self = .devicePaired(try payload(DevicePaired.self))
        case "welcome": self = .welcome(try payload(Welcome.self))
        case "status": self = .status(try payload(BridgeStatus.self))
        case "call.incoming": self = .callIncoming(try payload(IncomingCall.self))
        case "call.offer": self = .callOffer(try payload(SessionOffer.self))
        case "call.media": self = .callMedia(try payload(CallMedia.self))
        case "call.state": self = .callState(try payload(CallStateChange.self))
        case "call.ended": self = .callEnded(try payload(CallEnded.self))
        case "error": self = .error(try payload(SignalingErrorPayload.self))
        default: self = .unknown(type: type)
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(type, forKey: .type)
        switch self {
        case .hello(let payload): try container.encode(payload, forKey: .payload)
        case .deviceUpdate(let payload): try container.encode(payload, forKey: .payload)
        case .callAttach(let payload): try container.encode(payload, forKey: .payload)
        case .callDial(let payload): try container.encode(payload, forKey: .payload)
        case .callAnswer(let payload): try container.encode(payload, forKey: .payload)
        case .callAccept(let payload): try container.encode(payload, forKey: .payload)
        case .callHangup(let payload): try container.encode(payload, forKey: .payload)
        case .callDTMF(let payload): try container.encode(payload, forKey: .payload)
        case .pairCompanion(let payload): try container.encode(payload, forKey: .payload)
        case .devicePaired(let payload): try container.encode(payload, forKey: .payload)
        case .pairCompanionRequest(let payload): try container.encode(payload, forKey: .payload)
        case .welcome(let payload): try container.encode(payload, forKey: .payload)
        case .status(let payload): try container.encode(payload, forKey: .payload)
        case .callIncoming(let payload): try container.encode(payload, forKey: .payload)
        case .callOffer(let payload): try container.encode(payload, forKey: .payload)
        case .callMedia(let payload): try container.encode(payload, forKey: .payload)
        case .callState(let payload): try container.encode(payload, forKey: .payload)
        case .callEnded(let payload): try container.encode(payload, forKey: .payload)
        case .error(let payload): try container.encode(payload, forKey: .payload)
        case .deviceUnpair, .unknown: try container.encode(EmptyPayload(), forKey: .payload)
        }
    }
}
