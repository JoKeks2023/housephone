import Foundation

public enum SignalingCodingError: Error, Equatable {
    case notUTF8
}

/// JSON configuration shared by everything that talks to the bridge.
public enum SignalingCoding {
    public static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
        return encoder
    }

    public static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let string = try container.decode(String.self)
            guard let date = parseDate(string) else {
                throw DecodingError.dataCorruptedError(in: container, debugDescription: "Invalid ISO-8601 date: \(string)")
            }
            return date
        }
        return decoder
    }

    public static func encode(_ message: SignalingMessage) throws -> String {
        let data = try makeEncoder().encode(message)
        guard let text = String(data: data, encoding: .utf8) else { throw SignalingCodingError.notUTF8 }
        return text
    }

    public static func decode(_ text: String) throws -> SignalingMessage {
        try makeDecoder().decode(SignalingMessage.self, from: Data(text.utf8))
    }

    /// Accepts ISO-8601 with and without fractional seconds. Go's
    /// `time.Time` marshals fractional seconds whenever they are non-zero.
    static func parseDate(_ string: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: string) { return date }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: string)
    }
}
