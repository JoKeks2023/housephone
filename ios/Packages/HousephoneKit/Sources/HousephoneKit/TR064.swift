import Foundation

/// Why a direct TR-064 request to the FRITZ!Box failed (mode without
/// bridge, ADR-0005). The mapping follows the bridge's Go client.
public enum TR064Error: Error, Equatable, Sendable {
    case unreachable
    /// Wrong user or password (HTTP/UPnP 401).
    case authentication
    /// Logged in, but the user lacks the right for the action (UPnP 606).
    case notAllowed
    /// Feature switched off, e.g. the call list (UPnP 820, no URL).
    case unsupported
    /// The change needs a confirmation at the FRITZ!Box (UPnP 866, see
    /// `X_AVM-DE_Auth`).
    case secondFactorRequired
    /// Too many confirmations; FRITZ!OS blocks them for up to an hour (867).
    case secondFactorBlocked
    /// Another confirmation is running; retry within two minutes (868).
    case secondFactorBusy
    case invalidResponse(String)
}

/// TR-064 client for phonebook and call list, talking to the FRITZ!Box
/// directly: `GetSecurityPort` over plain HTTP (no login, only a port
/// number is accepted), then SOAP over TLS with HTTP digest auth.
///
/// The FRITZ!Box has a self-signed certificate for its own name, so it is
/// accepted for the configured host only; TLS keeps the data from passive
/// listeners on the LAN, digest auth protects the password.
public final class TR064Client: NSObject, Sendable, URLSessionTaskDelegate {
    public let host: String
    private let username: String
    private let password: String
    private let plainPort: Int
    private let timeout: TimeInterval
    private let securityPort = LockedValue<Int?>(nil)
    /// Bigger than any real phonebook, small enough for memory.
    static let maxDownloadBytes = 8 << 20

    static let onTelControl = "/upnp/control/x_contact"
    static let onTelService = "urn:dslforum-org:service:X_AVM-DE_OnTel:1"
    static let deviceInfoControl = "/upnp/control/deviceinfo"
    static let deviceInfoService = "urn:dslforum-org:service:DeviceInfo:1"
    static let voipControl = "/upnp/control/x_voip"
    static let voipService = "urn:dslforum-org:service:X_VoIP:1"
    static let authControl = "/upnp/control/x_auth"
    static let authService = "urn:dslforum-org:service:X_AVM-DE_Auth:1"

    public init(host: String, username: String, password: String, plainPort: Int = 49000, timeout: TimeInterval = 15) {
        self.host = host
        self.username = username
        self.password = password
        self.plainPort = plainPort
        self.timeout = timeout
    }

    private var session: URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = timeout
        configuration.allowsCellularAccess = false
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        return URLSession(configuration: configuration, delegate: self, delegateQueue: nil)
    }

    // MARK: - Phonebook and call list

    public func phonebook(now: Date = Date()) async throws(TR064Error) -> FritzBoxPhonebook {
        let list = try await call(Self.onTelControl, Self.onTelService, "GetPhonebookList")
        var contacts: [FritzBoxContact] = []
        for id in (list["NewPhonebookList"] ?? "").split(separator: ",").map({ $0.trimmingCharacters(in: .whitespaces) }) where !id.isEmpty {
            let book = try await call(Self.onTelControl, Self.onTelService, "GetPhonebook", [("NewPhonebookID", id)])
            guard let url = book["NewPhonebookURL"], !url.isEmpty else { continue }
            let data = try await download(url)
            contacts += try TR064XML.phonebook(data, id: id, name: book["NewPhonebookName"])
        }
        return FritzBoxPhonebook(updatedAt: now, contacts: TR064XML.sorted(contacts))
    }

    public func callList(limit: Int, now: Date = Date(), timeZone: TimeZone = .current) async throws(TR064Error) -> FritzBoxCallList {
        let values = try await call(Self.onTelControl, Self.onTelService, "GetCallList")
        guard let url = values["NewCallListURL"], !url.isEmpty else { throw .unsupported }
        let data = try await download(url, extra: [URLQueryItem(name: "max", value: String(limit))])
        return FritzBoxCallList(updatedAt: now, calls: Array(try TR064XML.callList(data, timeZone: timeZone).prefix(limit)))
    }

    // MARK: - SOAP

    private func secureBase() async throws(TR064Error) -> URLComponents {
        let port: Int
        if let known = securityPort.value {
            port = known
        } else {
            let values = try await soap(base: components(scheme: "http", port: plainPort), Self.deviceInfoControl, Self.deviceInfoService, "GetSecurityPort", [], token: nil)
            // Unauthenticated answer: accept a plain number only, so a
            // forged value cannot point the host elsewhere.
            guard let value = values["NewSecurityPort"].flatMap({ Int($0.trimmingCharacters(in: .whitespaces)) }), (1...65535).contains(value) else {
                throw .invalidResponse("GetSecurityPort")
            }
            securityPort.value = value
            port = value
        }
        return components(scheme: "https", port: port)
    }

    private func components(scheme: String, port: Int) -> URLComponents {
        var components = URLComponents()
        components.scheme = scheme
        components.host = host
        components.port = port
        return components
    }

    /// `token`: the second-factor token of `X_AVM-DE_Auth`, sent in the
    /// SOAP header once the user has confirmed at the FRITZ!Box.
    func call(_ control: String, _ service: String, _ action: String, _ arguments: [(String, String)] = [], token: String? = nil) async throws(TR064Error) -> [String: String] {
        let base = try await secureBase()
        do {
            return try await soap(base: base, control, service, action, arguments, token: token)
        } catch .unreachable {
            securityPort.value = nil
            throw .unreachable
        }
    }

    private func soap(base: URLComponents, _ control: String, _ service: String, _ action: String, _ arguments: [(String, String)], token: String?) async throws(TR064Error) -> [String: String] {
        var components = base
        components.path = control
        guard let url = components.url else { throw .invalidResponse("URL") }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue(#"text/xml; charset="utf-8""#, forHTTPHeaderField: "Content-Type")
        request.setValue("\"\(service)#\(action)\"", forHTTPHeaderField: "SOAPAction")
        request.httpBody = Data(TR064XML.envelope(action: action, service: service, arguments: arguments, token: token).utf8)

        let (data, response) = try await load(request)
        switch response.statusCode {
        case 200:
            guard let values = TR064XML.responseValues(data, element: action + "Response") else { throw .invalidResponse(action) }
            return values
        case 401:
            throw .authentication
        default:
            throw TR064XML.error(faultCode: TR064XML.faultCode(data), action: action, status: response.statusCode)
        }
    }

    /// Fetches a URL the FRITZ!Box returned. It carries a session ID; the
    /// host is replaced by ours so nothing is ever fetched elsewhere.
    private func download(_ raw: String, extra: [URLQueryItem] = []) async throws(TR064Error) -> Data {
        var base = try await secureBase()
        guard let source = URLComponents(string: raw.trimmingCharacters(in: .whitespaces)), !source.path.isEmpty else {
            throw .invalidResponse("download URL")
        }
        base.path = source.path
        base.queryItems = (source.queryItems ?? []).filter { item in !extra.contains { $0.name == item.name } } + extra
        guard let url = base.url else { throw .invalidResponse("download URL") }
        let (data, response) = try await load(URLRequest(url: url))
        switch response.statusCode {
        case 200:
            guard data.count <= Self.maxDownloadBytes else { throw .invalidResponse("download too large") }
            return data
        case 401, 403: throw .authentication
        default: throw .invalidResponse("download: HTTP \(response.statusCode)")
        }
    }

    private func load(_ request: URLRequest) async throws(TR064Error) -> (Data, HTTPURLResponse) {
        let session = session
        defer { session.finishTasksAndInvalidate() }
        do {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else { throw TR064Error.invalidResponse("not HTTP") }
            return (data, http)
        } catch let error as TR064Error {
            throw error
        } catch {
            throw .unreachable
        }
    }

    // MARK: - URLSessionTaskDelegate

    public func urlSession(_ session: URLSession, task: URLSessionTask, didReceive challenge: URLAuthenticationChallenge) async -> (URLSession.AuthChallengeDisposition, URLCredential?) {
        let space = challenge.protectionSpace
        guard space.host.caseInsensitiveCompare(host) == .orderedSame else { return (.cancelAuthenticationChallenge, nil) }
        switch space.authenticationMethod {
        case NSURLAuthenticationMethodServerTrust:
            guard let trust = space.serverTrust else { return (.cancelAuthenticationChallenge, nil) }
            return (.useCredential, URLCredential(trust: trust))
        case NSURLAuthenticationMethodHTTPDigest:
            // A second challenge means the password was wrong.
            guard challenge.previousFailureCount == 0 else { return (.rejectProtectionSpace, nil) }
            return (.useCredential, URLCredential(user: username, password: password, persistence: .none))
        default:
            // Never send the password with Basic auth.
            return (.rejectProtectionSpace, nil)
        }
    }

    public func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest) async -> URLRequest? {
        nil
    }
}

final class LockedValue<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: Value

    init(_ value: Value) { stored = value }

    var value: Value {
        get { lock.withLock { stored } }
        set { lock.withLock { stored = newValue } }
    }
}

// MARK: - XML

/// The TR-064 documents: SOAP envelopes, phonebook export (x_contactSCPD
/// 5.1), call list (5.2). Same rules as the bridge (`bridge/internal/fritzbox`).
public enum TR064XML {
    static func envelope(action: String, service: String, arguments: [(String, String)], token: String? = nil) -> String {
        var body = #"<?xml version="1.0" encoding="utf-8"?><s:Envelope s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/" xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">"#
        if let token {
            // X_AVM-DE_Auth 6.4: the token goes into the SOAP header.
            body += #"<s:Header><avm:token xmlns:avm="avm.de" s:mustUnderstand="1">"# + escape(token) + "</avm:token></s:Header>"
        }
        body += "<s:Body>"
        body += "<u:\(action) xmlns:u=\"\(service)\">"
        for (name, value) in arguments { body += "<\(name)>\(escape(value))</\(name)>" }
        body += "</u:\(action)></s:Body></s:Envelope>"
        return body
    }

    static func escape(_ text: String) -> String {
        text.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;").replacingOccurrences(of: "\"", with: "&quot;")
    }

    /// Child elements of `<element>` (e.g. `GetPhonebookResponse`).
    public static func responseValues(_ data: Data, element: String) -> [String: String]? {
        let tree = XMLTree.parse(data)
        guard let response = tree?.first(named: element) else { return nil }
        return Dictionary(response.children.map { ($0.name, $0.text.trimmingCharacters(in: .whitespacesAndNewlines)) }, uniquingKeysWith: { first, _ in first })
    }

    /// UPnP error codes of a SOAP fault (TR-064 First Steps 6.1, X_VoIP,
    /// X_AVM-DE_Auth 6.3).
    static func error(faultCode: String?, action: String, status: Int) -> TR064Error {
        switch faultCode {
        case "401": .authentication
        case "606": .notAllowed
        case "820": .unsupported
        case "866": .secondFactorRequired
        case "867": .secondFactorBlocked
        case "868": .secondFactorBusy
        default: .invalidResponse("\(action): HTTP \(status)" + (faultCode.map { ", UPnP \($0)" } ?? ""))
        }
    }

    static func faultCode(_ data: Data) -> String? {
        XMLTree.parse(data)?.first(named: "UPnPError")?.child("errorCode")?.text.trimmingCharacters(in: .whitespaces)
    }

    public static func phonebook(_ data: Data, id: String, name: String?) throws(TR064Error) -> [FritzBoxContact] {
        guard let tree = XMLTree.parse(data) else { throw .invalidResponse("phonebook \(id)") }
        var contacts: [FritzBoxContact] = []
        var index = 0
        for book in tree.all(named: "phonebook") {
            let bookName = name?.trimmingCharacters(in: .whitespaces).nonEmpty ?? book.attributes["name"]?.trimmingCharacters(in: .whitespaces)
            for contact in book.children where contact.name == "contact" {
                index += 1
                let numbers: [FritzBoxNumber] = (contact.child("telephony")?.children ?? []).compactMap { element in
                    guard element.name == "number" else { return nil }
                    var type = (element.attributes["type"] ?? "").trimmingCharacters(in: .whitespaces).lowercased()
                    if type == FritzBoxNumberType.faxWork.rawValue { return nil }
                    if !knownNumberTypes.contains(type) { type = FritzBoxNumberType.other.rawValue }
                    let number = cleanNumber(element.text)
                    guard !number.isEmpty else { return nil }
                    return FritzBoxNumber(number: number, type: FritzBoxNumberType(rawValue: type), preferred: element.attributes["prio"]?.trimmingCharacters(in: .whitespaces) == "1")
                }
                guard let first = numbers.first else { continue }
                let uniqueID = contact.child("uniqueid")?.text.trimmingCharacters(in: .whitespaces).nonEmpty ?? "n\(index)"
                let realName = contact.child("person")?.child("realName")?.text.trimmingCharacters(in: .whitespacesAndNewlines).nonEmpty
                contacts.append(FritzBoxContact(
                    id: "\(id)-\(uniqueID)",
                    name: realName ?? first.number,
                    favorite: contact.child("category")?.text.trimmingCharacters(in: .whitespaces) == "1",
                    phonebook: bookName,
                    numbers: numbers
                ))
            }
        }
        return contacts
    }

    static func sorted(_ contacts: [FritzBoxContact]) -> [FritzBoxContact] {
        contacts.sorted { lhs, rhs in
            let a = lhs.name.lowercased(), b = rhs.name.lowercased()
            return a != b ? a < b : lhs.id < rhs.id
        }
    }

    private static let knownNumberTypes: Set<String> = ["home", "mobile", "work", "intern", "memo", "other"]

    /// x_contactSCPD table 70.
    private static let callTypes: [String: (FritzBoxCall.Direction, FritzBoxCall.Result)] = [
        "1": (.incoming, .answered),
        "2": (.incoming, .missed),
        "3": (.outgoing, .answered),
        "9": (.incoming, .active),
        "10": (.incoming, .rejected),
        "11": (.outgoing, .active),
    ]

    /// Newest first. Fax calls (port 5) are dropped; ports 6 and 40–49 are
    /// answering machines.
    public static func callList(_ data: Data, timeZone: TimeZone) throws(TR064Error) -> [FritzBoxCall] {
        guard let tree = XMLTree.parse(data) else { throw .invalidResponse("call list") }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = timeZone
        formatter.dateFormat = "dd.MM.yy HH:mm"

        var calls: [FritzBoxCall] = []
        for element in tree.all(named: "Call") {
            func text(_ name: String) -> String { element.child(name)?.text.trimmingCharacters(in: .whitespacesAndNewlines) ?? "" }
            guard let (direction, result) = callTypes[text("Type")] else { continue }
            let port = text("Port")
            guard port != "5", let started = formatter.date(from: text("Date")) else { continue }
            var answeredBy: FritzBoxCall.AnsweredBy?
            if direction == .incoming, result == .answered {
                let number = Int(port)
                answeredBy = number == 6 || (40...49).contains(number ?? -1) ? .answeringMachine : .phone
            }
            calls.append(FritzBoxCall(
                id: text("Id"),
                direction: direction,
                result: result,
                number: cleanNumber(direction == .incoming ? text("Caller") : text("Called")),
                name: text("Name").nonEmpty,
                device: text("Device").nonEmpty,
                answeredBy: answeredBy,
                startedAt: started,
                durationSeconds: durationSeconds(text("Duration"))
            ))
        }
        return calls.enumerated().sorted { lhs, rhs in
            if lhs.element.startedAt != rhs.element.startedAt { return lhs.element.startedAt > rhs.element.startedAt }
            let a = Int(lhs.element.id) ?? 0, b = Int(rhs.element.id) ?? 0
            return a != b ? a > b : lhs.offset < rhs.offset
        }.map(\.element)
    }

    /// "h:mm", minutes rounded up by the FRITZ!Box.
    static func durationSeconds(_ text: String) -> Int {
        let parts = text.split(separator: ":")
        guard parts.count == 2, let hours = Int(parts[0]), let minutes = Int(parts[1]), hours >= 0, minutes >= 0 else { return 0 }
        return (hours * 60 + minutes) * 60
    }

    /// Keeps what is dialable: a leading "+", digits, "*" and "#".
    static func cleanNumber(_ text: String) -> String {
        var result = ""
        for character in text.trimmingCharacters(in: .whitespaces) {
            if character.isASCII, character.isNumber || character == "*" || character == "#" {
                result.append(character)
            } else if character == "+", result.isEmpty {
                result.append(character)
            }
        }
        return result
    }
}

/// Minimal DOM over `XMLParser`, enough for the small TR-064 documents.
final class XMLTree {
    let name: String
    let attributes: [String: String]
    fileprivate(set) var children: [XMLTree] = []
    fileprivate(set) var text = ""

    init(name: String, attributes: [String: String]) {
        self.name = name
        self.attributes = attributes
    }

    func child(_ name: String) -> XMLTree? { children.first { $0.name == name } }

    func first(named name: String) -> XMLTree? {
        if self.name == name { return self }
        for child in children {
            if let found = child.first(named: name) { return found }
        }
        return nil
    }

    func all(named name: String) -> [XMLTree] {
        (self.name == name ? [self] : []) + children.flatMap { $0.all(named: name) }
    }

    static func parse(_ data: Data) -> XMLTree? {
        let builder = Builder()
        let parser = XMLParser(data: data)
        parser.shouldProcessNamespaces = true
        parser.shouldResolveExternalEntities = false
        parser.delegate = builder
        guard parser.parse() else { return nil }
        return builder.root
    }

    private final class Builder: NSObject, XMLParserDelegate {
        var root: XMLTree?
        private var stack: [XMLTree] = []

        func parser(_ parser: XMLParser, didStartElement elementName: String, namespaceURI: String?, qualifiedName: String?, attributes: [String: String] = [:]) {
            let node = XMLTree(name: elementName, attributes: attributes)
            stack.last?.children.append(node)
            if root == nil { root = node }
            stack.append(node)
        }

        func parser(_ parser: XMLParser, didEndElement elementName: String, namespaceURI: String?, qualifiedName: String?) {
            _ = stack.popLast()
        }

        func parser(_ parser: XMLParser, foundCharacters string: String) {
            stack.last?.text += string
        }

        func parser(_ parser: XMLParser, foundCDATA CDATABlock: Data) {
            stack.last?.text += String(decoding: CDATABlock, as: UTF8.self)
        }
    }
}

extension String {
    var nonEmpty: String? { isEmpty ? nil : self }
}
