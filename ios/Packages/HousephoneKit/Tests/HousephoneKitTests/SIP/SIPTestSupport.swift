import Foundation
@testable import HousephoneKit

struct TimedOut: Error, CustomStringConvertible {
    let what: String
    var description: String { "Timed out waiting for \(what)" }
}

/// Plays the FRITZ!Box: records what the user agent sends and delivers
/// datagrams back to it.
final class FakeSIPTransport: SIPTransport, @unchecked Sendable {
    let incoming: AsyncStream<Data>
    private let continuation: AsyncStream<Data>.Continuation
    private let lock = NSLock()
    private var sent: [SIPMessage] = []
    private var consumed = Set<Int>()
    private let endpoint: (host: String, port: UInt16)?

    init(endpoint: (host: String, port: UInt16)? = ("192.0.2.10", 5062)) {
        self.endpoint = endpoint
        (incoming, continuation) = AsyncStream.makeStream()
    }

    func localEndpoint() async -> (host: String, port: UInt16)? { endpoint }

    func send(_ datagram: Data) async throws {
        let message = try SIPMessage(parsing: datagram)
        lock.withLock { sent.append(message) }
    }

    func close() { continuation.finish() }

    func deliver(_ message: SIPMessage) {
        continuation.yield(message.serialized())
    }

    var allSent: [SIPMessage] { lock.withLock { sent } }

    /// The oldest message not yet taken that matches.
    func next(_ what: String, timeout: Duration = .seconds(3), where predicate: (SIPMessage) -> Bool) async throws -> SIPMessage {
        let deadline = ContinuousClock.now + timeout
        while ContinuousClock.now < deadline {
            let match: SIPMessage? = lock.withLock {
                guard let index = sent.indices.first(where: { !consumed.contains($0) && predicate(sent[$0]) }) else { return nil }
                consumed.insert(index)
                return sent[index]
            }
            if let match { return match }
            try await Task.sleep(for: .milliseconds(2))
        }
        throw TimedOut(what: what)
    }

    func nextRequest(_ method: String, timeout: Duration = .seconds(3)) async throws -> SIPMessage {
        try await next(method, timeout: timeout) { $0.method == method }
    }

    func nextResponse(_ status: Int, method: String, timeout: Duration = .seconds(3)) async throws -> SIPMessage {
        try await next("\(status) to \(method)", timeout: timeout) { $0.status == status && $0.cseq?.method == method }
    }

    func count(where predicate: (SIPMessage) -> Bool) -> Int {
        lock.withLock { sent.filter(predicate).count }
    }
}

/// Collects user-agent events for polling assertions.
final class EventRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var events: [SIPEvent] = []
    private var consumed = Set<Int>()
    private var task: Task<Void, Never>?

    init(_ stream: AsyncStream<SIPEvent>) {
        task = Task { [weak self] in
            for await event in stream { self?.lock.withLock { self?.events.append(event) } }
        }
    }

    deinit { task?.cancel() }

    var all: [SIPEvent] { lock.withLock { events } }

    func next(_ what: String, timeout: Duration = .seconds(3), where predicate: (SIPEvent) -> Bool) async throws -> SIPEvent {
        let deadline = ContinuousClock.now + timeout
        while ContinuousClock.now < deadline {
            let match: SIPEvent? = lock.withLock {
                guard let index = events.indices.first(where: { !consumed.contains($0) && predicate(events[$0]) }) else { return nil }
                consumed.insert(index)
                return events[index]
            }
            if let match { return match }
            try await Task.sleep(for: .milliseconds(2))
        }
        throw TimedOut(what: what)
    }
}

enum FakeFritzBox {
    static let host = "192.0.2.1"

    static func response(to request: SIPMessage, _ status: Int, _ reason: String, toTag: String? = nil, headers extra: [(String, String)] = [], body: Data = Data()) -> SIPMessage {
        var headers = SIPHeaders()
        for via in request.headers.rawValues("Via") { headers.add("Via", via + ";received=192.0.2.10") }
        headers["From"] = request.headers["From"]
        var to = request.headers["To"] ?? ""
        if let toTag, request.to?.tag == nil { to += ";tag=\(toTag)" }
        headers.add("To", to)
        headers["Call-ID"] = request.headers["Call-ID"]
        headers["CSeq"] = request.headers["CSeq"]
        for (name, value) in extra { headers.add(name, value) }
        if !body.isEmpty { headers.add("Content-Type", "application/sdp") }
        return SIPMessage(startLine: .response(status: status, reason: reason), headers: headers, body: body)
    }

    static func challenge(_ request: SIPMessage, status: Int = 401, nonce: String = "4a1b2c3d", stale: Bool = false) -> SIPMessage {
        let name = status == 401 ? "WWW-Authenticate" : "Proxy-Authenticate"
        return response(to: request, status, "Unauthorized", headers: [
            (name, "Digest realm=\"fritz.box\", nonce=\"\(nonce)\", algorithm=MD5, qop=\"auth\"\(stale ? ", stale=true" : "")"),
        ])
    }

    /// Checks a digest answer the way the FRITZ!Box would.
    static func isAuthorized(_ request: SIPMessage, header: String = "Authorization", password: String) -> Bool {
        guard let value = request.headers.rawValues(header).first, value.hasPrefix("Digest ") else { return false }
        let parameters = SIPDigestChallenge.parameters(String(value.dropFirst(7)))
        guard let username = parameters["username"], let realm = parameters["realm"], let nonce = parameters["nonce"],
              let uri = parameters["uri"], let response = parameters["response"], let method = request.method
        else { return false }
        let ha1 = SIPDigest.md5Hex("\(username):\(realm):\(password)")
        let ha2 = SIPDigest.md5Hex("\(method):\(uri)")
        let expected: String
        if parameters["qop"] == "auth", let nc = parameters["nc"], let cnonce = parameters["cnonce"] {
            expected = SIPDigest.md5Hex("\(ha1):\(nonce):\(nc):\(cnonce):auth:\(ha2)")
        } else {
            expected = SIPDigest.md5Hex("\(ha1):\(nonce):\(ha2)")
        }
        return response == expected && uri == request.requestURI
    }

    static let offer = """
    v=0\r
    o=user 1234 1234 IN IP4 192.0.2.1\r
    s=call\r
    c=IN IP4 192.0.2.1\r
    t=0 0\r
    m=audio 7078 RTP/AVP 9 8 0 101\r
    a=rtpmap:9 G722/8000\r
    a=rtpmap:8 PCMA/8000\r
    a=rtpmap:0 PCMU/8000\r
    a=rtpmap:101 telephone-event/8000\r
    a=fmtp:101 0-15\r
    a=sendrecv\r
    a=ptime:20\r

    """

    static func invite(callID: String = "fb-call-1@192.0.2.1", from number: String = "5550100", name: String? = "Test Anrufer", body: String = offer) -> SIPMessage {
        let display = name.map { "\"\($0)\" " } ?? ""
        return .request("INVITE", uri: "sip:620@192.0.2.10:5062", headers: SIPHeaders([
            ("Via", "SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bKfb\(callID.hashValue & 0xFFFF)"),
            ("From", "\(display)<sip:\(number)@fritz.box>;tag=fbtag1"),
            ("To", "<sip:620@fritz.box>"),
            ("Call-ID", callID),
            ("CSeq", "1 INVITE"),
            ("Contact", "<sip:\(number)@192.0.2.1:5060>"),
            ("Max-Forwards", "70"),
            ("Content-Type", "application/sdp"),
        ]), body: Data(body.utf8))
    }

    /// A request from the FRITZ!Box inside the dialog of `invite`.
    static func inDialog(_ method: String, invite: SIPMessage, ourTag: String?, cseq: Int = 2, branch: String = "z9hG4bKfbx") -> SIPMessage {
        var to = invite.headers["To"] ?? ""
        if let ourTag { to += ";tag=\(ourTag)" }
        return .request(method, uri: "sip:620@192.0.2.10:5062", headers: SIPHeaders([
            ("Via", "SIP/2.0/UDP 192.0.2.1:5060;branch=\(branch)"),
            ("From", invite.headers["From"] ?? ""),
            ("To", to),
            ("Call-ID", invite.callID ?? ""),
            ("CSeq", "\(cseq) \(method)"),
            ("Max-Forwards", "70"),
        ]))
    }
}
