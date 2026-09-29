import Foundation

/// A UUID that the signaling protocol always writes in lowercase.
///
/// Foundation encodes `UUID` in uppercase, but the bridge compares IDs as
/// strings, so every ID on the wire goes through this type.
public struct ProtocolUUID<Kind>: Hashable, Sendable, Codable, CustomStringConvertible {
    public let uuid: UUID

    public init(_ uuid: UUID = UUID()) {
        self.uuid = uuid
    }

    public init?(string: String) {
        guard let uuid = UUID(uuidString: string) else { return nil }
        self.uuid = uuid
    }

    public var description: String { uuid.uuidString.lowercased() }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        let string = try container.decode(String.self)
        guard let uuid = UUID(uuidString: string) else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Invalid UUID: \(string)")
        }
        self.uuid = uuid
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(description)
    }
}

public enum CallIDKind {}
public enum DeviceIDKind {}

/// Identifies one call. Devices use it directly as the CallKit call UUID.
public typealias CallID = ProtocolUUID<CallIDKind>

/// Identifies one paired device at the bridge.
public typealias DeviceID = ProtocolUUID<DeviceIDKind>
