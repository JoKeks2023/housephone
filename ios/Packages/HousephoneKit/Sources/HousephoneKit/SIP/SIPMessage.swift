import Foundation

/// A SIP request or response (RFC 3261 §7), parsed leniently and
/// serialized strictly (CRLF, `Content-Length` always set).
public struct SIPMessage: Sendable, Equatable {
    public enum StartLine: Sendable, Equatable {
        case request(method: String, uri: String)
        case response(status: Int, reason: String)
    }

    public var startLine: StartLine
    public var headers: SIPHeaders
    public var body: Data

    public init(startLine: StartLine, headers: SIPHeaders = SIPHeaders(), body: Data = Data()) {
        self.startLine = startLine
        self.headers = headers
        self.body = body
    }

    public static func request(_ method: String, uri: String, headers: SIPHeaders = SIPHeaders(), body: Data = Data()) -> SIPMessage {
        SIPMessage(startLine: .request(method: method, uri: uri), headers: headers, body: body)
    }

    public var method: String? {
        if case .request(let method, _) = startLine { method } else { nil }
    }

    public var requestURI: String? {
        if case .request(_, let uri) = startLine { uri } else { nil }
    }

    public var status: Int? {
        if case .response(let status, _) = startLine { status } else { nil }
    }

    public var isRequest: Bool { method != nil }

    // MARK: - Common headers

    public var callID: String? { headers["Call-ID"] }

    public var cseq: (number: UInt32, method: String)? {
        guard let value = headers["CSeq"] else { return nil }
        let parts = value.split(whereSeparator: \.isWhitespace)
        guard parts.count == 2, let number = UInt32(parts[0]) else { return nil }
        return (number, parts[1].uppercased())
    }

    /// The branch of the topmost Via, the transaction key (RFC 3261 §17).
    public var topViaBranch: String? {
        guard let via = headers.values("Via").first else { return nil }
        return SIPHeaders.parameter("branch", in: via)
    }

    public var from: SIPAddress? { headers["From"].flatMap(SIPAddress.init(parsing:)) }
    public var to: SIPAddress? { headers["To"].flatMap(SIPAddress.init(parsing:)) }
    public var contact: SIPAddress? { headers.values("Contact").first.flatMap(SIPAddress.init(parsing:)) }

    // MARK: - Parsing

    public enum ParseError: Error, Equatable {
        case notUTF8
        case missingStartLine
        case invalidStartLine(String)
        case invalidHeader(String)
        case truncatedBody(expected: Int, actual: Int)
    }

    /// Parses one datagram. Accepts bare LF line ends and folded headers;
    /// the body is cut to `Content-Length` when present.
    public init(parsing data: Data) throws(ParseError) {
        let separator = Self.headerEnd(in: data)
        let headData = data[data.startIndex..<(separator?.lowerBound ?? data.endIndex)]
        var bodyData = separator.map { data[$0.upperBound...] } ?? Data()

        guard let head = String(data: headData, encoding: .utf8) else { throw .notUTF8 }
        var lines = head.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        while lines.first?.isEmpty == true { lines.removeFirst() }
        guard let first = lines.first, !first.isEmpty else { throw .missingStartLine }
        startLine = try Self.parseStartLine(first)

        var headers = SIPHeaders()
        for line in lines.dropFirst() where !line.isEmpty {
            if line.first == " " || line.first == "\t" {
                // Folded continuation of the previous header (§7.3.1).
                guard !headers.fields.isEmpty else { throw .invalidHeader(line) }
                headers.fields[headers.fields.count - 1].value += " " + line.trimmingCharacters(in: .whitespaces)
                continue
            }
            guard let colon = line.firstIndex(of: ":") else { throw .invalidHeader(line) }
            let name = line[..<colon].trimmingCharacters(in: .whitespaces)
            guard !name.isEmpty, !name.contains(" ") else { throw .invalidHeader(line) }
            headers.add(name, line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces))
        }
        self.headers = headers

        if let lengthValue = headers["Content-Length"], let length = Int(lengthValue), length >= 0 {
            guard bodyData.count >= length else { throw .truncatedBody(expected: length, actual: bodyData.count) }
            bodyData = bodyData.prefix(length)
        }
        body = Data(bodyData)
    }

    private static func headerEnd(in data: Data) -> Range<Data.Index>? {
        let crlf = data.range(of: Data("\r\n\r\n".utf8))
        let lf = data.range(of: Data("\n\n".utf8))
        switch (crlf, lf) {
        case let (crlf?, lf?): return crlf.lowerBound <= lf.lowerBound ? crlf : lf
        case let (crlf?, nil): return crlf
        case let (nil, lf?): return lf
        case (nil, nil): return nil
        }
    }

    private static func parseStartLine(_ line: String) throws(ParseError) -> StartLine {
        let parts = line.split(separator: " ", maxSplits: 2, omittingEmptySubsequences: true)
        guard parts.count >= 2 else { throw .invalidStartLine(line) }
        if parts[0].uppercased().hasPrefix("SIP/") {
            guard let status = Int(parts[1]), (100...699).contains(status) else { throw .invalidStartLine(line) }
            return .response(status: status, reason: parts.count == 3 ? String(parts[2]) : "")
        }
        guard parts.count == 3, parts[2].uppercased() == "SIP/2.0",
              parts[0].allSatisfy({ $0.isLetter || $0 == "-" })
        else { throw .invalidStartLine(line) }
        return .request(method: parts[0].uppercased(), uri: String(parts[1]))
    }

    // MARK: - Serializing

    public func serialized() -> Data {
        var text: String
        switch startLine {
        case .request(let method, let uri): text = "\(method) \(uri) SIP/2.0\r\n"
        case .response(let status, let reason): text = "SIP/2.0 \(status) \(reason)\r\n"
        }
        for field in headers.fields where SIPHeaders.canonical(field.name) != "content-length" {
            text += "\(field.name): \(field.value)\r\n"
        }
        text += "Content-Length: \(body.count)\r\n\r\n"
        return Data(text.utf8) + body
    }
}

/// Ordered header fields; lookups are case-insensitive and know the
/// compact forms (§7.3.3).
public struct SIPHeaders: Sendable, Equatable {
    public struct Field: Sendable, Equatable {
        public var name: String
        public var value: String
    }

    public var fields: [Field] = []

    public init() {}

    public init(_ pairs: [(String, String)]) {
        fields = pairs.map { Field(name: $0.0, value: $0.1) }
    }

    private static let compactForms: [String: String] = [
        "v": "via", "f": "from", "t": "to", "i": "call-id", "m": "contact",
        "l": "content-length", "c": "content-type", "k": "supported", "e": "content-encoding", "s": "subject",
    ]

    static func canonical(_ name: String) -> String {
        let lower = name.lowercased()
        return compactForms[lower] ?? lower
    }

    /// The first value of `name`.
    public subscript(_ name: String) -> String? {
        get { values(name).first }
        set {
            remove(name)
            if let newValue { add(name, newValue) }
        }
    }

    /// All values of `name`, with comma-separated lists split (outside of
    /// quotes and angle brackets). Not for headers whose values may
    /// contain commas themselves (auth challenges).
    public func values(_ name: String) -> [String] {
        let key = Self.canonical(name)
        return fields.filter { Self.canonical($0.name) == key }.flatMap { Self.splitList($0.value) }
    }

    /// Raw field values of `name`, one per header line.
    public func rawValues(_ name: String) -> [String] {
        let key = Self.canonical(name)
        return fields.filter { Self.canonical($0.name) == key }.map(\.value)
    }

    public mutating func add(_ name: String, _ value: String) {
        fields.append(Field(name: name, value: value))
    }

    public mutating func remove(_ name: String) {
        let key = Self.canonical(name)
        fields.removeAll { Self.canonical($0.name) == key }
    }

    static func splitList(_ value: String) -> [String] {
        var parts: [String] = []
        var current = ""
        var inQuotes = false
        var inAngle = false
        var previous: Character?
        for character in value {
            switch character {
            case "\"" where previous != "\\": inQuotes.toggle()
            case "<" where !inQuotes: inAngle = true
            case ">" where !inQuotes: inAngle = false
            case "," where !inQuotes && !inAngle:
                parts.append(current.trimmingCharacters(in: .whitespaces))
                current = ""
                previous = character
                continue
            default: break
            }
            current.append(character)
            previous = character
        }
        let last = current.trimmingCharacters(in: .whitespaces)
        if !last.isEmpty { parts.append(last) }
        return parts
    }

    /// Value of a `;name=value` parameter in a header value, `""` for a
    /// flag parameter without value. Ignores parameters inside `<…>`.
    public static func parameter(_ name: String, in value: String) -> String? {
        var outside = value
        if let close = value.lastIndex(of: ">") { outside = String(value[value.index(after: close)...]) }
        for part in outside.split(separator: ";").dropFirst(outside.hasPrefix(";") ? 0 : 1) {
            let pair = part.split(separator: "=", maxSplits: 1)
            guard let key = pair.first?.trimmingCharacters(in: .whitespaces), key.caseInsensitiveEquals(name) else { continue }
            return pair.count == 2 ? pair[1].trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "\"")) : ""
        }
        return nil
    }
}

/// A name-addr or addr-spec with header parameters (`From`, `To`,
/// `Contact`, `Record-Route`), e.g. `"Anna" <sip:030123@fritz.box>;tag=ab`.
public struct SIPAddress: Sendable, Equatable {
    public var displayName: String?
    public var uri: String
    /// Header parameters after the address, in order.
    public var parameters: [(key: String, value: String?)]

    public init(displayName: String? = nil, uri: String, parameters: [(key: String, value: String?)] = []) {
        self.displayName = displayName
        self.uri = uri
        self.parameters = parameters
    }

    public init?(parsing value: String) {
        let text = value.trimmingCharacters(in: .whitespaces)
        var rest: Substring
        if let open = text.firstIndex(of: "<"), let close = text[open...].firstIndex(of: ">") {
            var name = text[..<open].trimmingCharacters(in: .whitespaces)
            if name.hasPrefix("\""), name.hasSuffix("\""), name.count >= 2 {
                name = String(name.dropFirst().dropLast()).replacingOccurrences(of: "\\\"", with: "\"")
            }
            displayName = name.isEmpty ? nil : name
            uri = String(text[text.index(after: open)..<close])
            rest = text[text.index(after: close)...]
        } else {
            // addr-spec: parameters after the URI belong to the header.
            displayName = nil
            let semicolon = text.firstIndex(of: ";") ?? text.endIndex
            uri = String(text[..<semicolon])
            rest = text[semicolon...]
        }
        guard !uri.isEmpty else { return nil }
        parameters = rest.split(separator: ";").compactMap { part in
            let pair = part.split(separator: "=", maxSplits: 1)
            guard let key = pair.first?.trimmingCharacters(in: .whitespaces), !key.isEmpty else { return nil }
            return (key, pair.count == 2 ? pair[1].trimmingCharacters(in: .whitespaces) : nil)
        }
    }

    public var tag: String? {
        get { parameters.first { $0.key.caseInsensitiveEquals("tag") }?.value }
        set {
            parameters.removeAll { $0.key.caseInsensitiveEquals("tag") }
            if let newValue { parameters.append(("tag", newValue)) }
        }
    }

    public func parameter(_ name: String) -> String? {
        parameters.first { $0.key.caseInsensitiveEquals(name) }?.value
    }

    /// The user part of a `sip:` / `sips:` / `tel:` URI, percent-decoded.
    public var user: String? { SIPURI.user(of: uri) }

    public static func == (lhs: SIPAddress, rhs: SIPAddress) -> Bool {
        lhs.displayName == rhs.displayName && lhs.uri == rhs.uri
            && lhs.parameters.map(\.key) == rhs.parameters.map(\.key)
            && lhs.parameters.map(\.value) == rhs.parameters.map(\.value)
    }

    public var headerValue: String {
        var text = ""
        if let displayName {
            text = "\"\(displayName.replacingOccurrences(of: "\"", with: "\\\""))\" "
        }
        text += "<\(uri)>"
        for (key, value) in parameters {
            text += value.map { ";\(key)=\($0)" } ?? ";\(key)"
        }
        return text
    }
}

public enum SIPURI {
    /// `sip:<user>@<host>`; `#` and other reserved characters in the user
    /// part are percent-encoded (e.g. `*31#` codes).
    public static func make(user: String?, host: String, port: UInt16? = nil) -> String {
        var uri = "sip:"
        if let user, !user.isEmpty {
            var allowed = CharacterSet.alphanumerics
            allowed.insert(charactersIn: "-_.!~*'()&=+$,;?/")
            uri += (user.addingPercentEncoding(withAllowedCharacters: allowed) ?? user) + "@"
        }
        uri += host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
        if let port { uri += ":\(port)" }
        return uri
    }

    public static func user(of uri: String) -> String? {
        let lower = uri.lowercased()
        var rest: Substring
        if lower.hasPrefix("sips:") { rest = uri.dropFirst(5) }
        else if lower.hasPrefix("sip:") { rest = uri.dropFirst(4) }
        else if lower.hasPrefix("tel:") {
            rest = uri.dropFirst(4)
            return String(rest.prefix { $0 != ";" }).removingPercentEncoding
        } else { return nil }
        guard let at = rest.firstIndex(of: "@") else { return nil }
        let userInfo = rest[..<at]
        let user = userInfo.prefix { $0 != ":" && $0 != ";" }
        return String(user).removingPercentEncoding ?? String(user)
    }
}

extension StringProtocol {
    func caseInsensitiveEquals(_ other: some StringProtocol) -> Bool {
        lowercased() == other.lowercased()
    }
}
