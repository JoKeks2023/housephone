import Foundation

public enum IncomingCallPushError: Error, Equatable {
    case notAnIncomingCall(type: String)
    case malformed
}

/// Payload of the VoIP push the bridge sends for an incoming call.
public struct IncomingCallPush: Codable, Sendable, Equatable {
    public var type: String
    public var v: Int
    public var callId: CallID
    public var caller: String
    public var callerName: String?
    public var bridgeId: String

    public init(callId: CallID, caller: String, callerName: String?, bridgeId: String) {
        self.type = "incoming_call"
        self.v = 1
        self.callId = callId
        self.caller = caller
        self.callerName = callerName
        self.bridgeId = bridgeId
    }

    /// Parses `PKPushPayload.dictionaryPayload`.
    public init(dictionary: [AnyHashable: Any]) throws {
        let stringKeyed = Dictionary(uniqueKeysWithValues: dictionary.compactMap { key, value in
            (key as? String).map { ($0, value) }
        })
        guard JSONSerialization.isValidJSONObject(stringKeyed),
              let data = try? JSONSerialization.data(withJSONObject: stringKeyed)
        else { throw IncomingCallPushError.malformed }
        try self.init(data: data)
    }

    public init(data: Data) throws {
        let decoded: IncomingCallPush
        do {
            decoded = try SignalingCoding.makeDecoder().decode(IncomingCallPush.self, from: data)
        } catch {
            throw IncomingCallPushError.malformed
        }
        guard decoded.type == "incoming_call" else { throw IncomingCallPushError.notAnIncomingCall(type: decoded.type) }
        self = decoded
    }

    /// `nil` when the caller withheld their number.
    public var callerNumber: String? {
        let trimmed = caller.trimmingCharacters(in: .whitespaces)
        return trimmed.isEmpty ? nil : trimmed
    }
}
