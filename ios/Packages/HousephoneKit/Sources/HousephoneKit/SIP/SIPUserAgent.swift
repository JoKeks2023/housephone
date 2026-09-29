import Foundation

/// The IP-phone account on the FRITZ!Box (Telefonie → Telefoniegeräte →
/// LAN/WLAN (IP-Telefon)).
public struct SIPAccount: Sendable, Equatable {
    public var registrar: String
    public var port: UInt16
    public var username: String
    public var password: String

    public init(registrar: String, port: UInt16 = 5060, username: String, password: String) {
        self.registrar = registrar
        self.port = port
        self.username = username
        self.password = password
    }
}

public struct SIPTimers: Sendable, Equatable {
    /// RTT estimate (RFC 3261 §17.1.1.1): 500 ms on a LAN.
    public var t1: Duration
    /// Cap of the non-INVITE retransmit interval.
    public var t2: Duration

    public init(t1: Duration = .milliseconds(500), t2: Duration = .seconds(4)) {
        self.t1 = t1
        self.t2 = t2
    }

    /// Timer B/F/H: 64·T1.
    public var timeout: Duration { t1 * 64 }
}

public struct SIPUserAgentConfiguration: Sendable {
    public var timers = SIPTimers()
    /// Requested registration lifetime in seconds.
    public var registerExpires = 600
    /// Pause before registering again after a failure (not after a
    /// rejected password: repeated bad logins make the FRITZ!Box block).
    public var retryAfterFailure: Duration = .seconds(30)
    public var userAgent = "Housephone"
    public var rtpPort: @Sendable () -> UInt16 = RTPSession.randomPort

    public init() {}
}

public enum SIPRegistrationState: Sendable, Equatable {
    case unregistered
    case registering
    case registered(expires: Int)
    case failed(SIPFailure)
}

public enum SIPFailure: Error, Sendable, Equatable {
    /// Wrong user name or password (401/403 after authenticating).
    case authentication
    /// No answer from the FRITZ!Box.
    case timeout
    /// No route, e.g. not on the home Wi-Fi.
    case transport
    case rejected(Int)
}

public enum SIPCallEndReason: Sendable, Equatable {
    case localHangUp
    case remoteHangUp
    /// The caller gave up before we answered.
    case remoteCancelled
    case busy
    case declined
    case notFound
    case unanswered
    case noCommonCodec
    case timeout
    case failed(Int)
}

public struct SIPIncomingCall: Sendable, Equatable {
    public var id: String
    /// Empty for withheld numbers.
    public var number: String
    public var displayName: String?
}

public enum SIPEvent: Sendable, Equatable {
    case registration(SIPRegistrationState)
    case incoming(SIPIncomingCall)
    /// 180/183; `earlyMedia` when the 183 carried SDP (ringback, announcements).
    case ringing(callID: String, earlyMedia: SDPMedia?, localRTPPort: UInt16)
    case connected(callID: String, media: SDPMedia, localRTPPort: UInt16)
    case ended(callID: String, reason: SIPCallEndReason)
}

/// A SIP user agent for one account on a FRITZ!Box: registration with
/// refresh, calls in both directions, one call at a time.
///
/// Implements the transaction layer of RFC 3261 §17 over an unreliable
/// transport (Timer A/B for INVITE, E/F for other requests, G/H for our
/// final responses) and digest authentication. No TCP/TLS, no forking,
/// no hold; re-INVITEs are answered with the unchanged SDP.
public actor SIPUserAgent {
    public nonisolated let events: AsyncStream<SIPEvent>
    private nonisolated let eventContinuation: AsyncStream<SIPEvent>.Continuation

    public let account: SIPAccount
    private let transport: any SIPTransport
    private let configuration: SIPUserAgentConfiguration
    private var timers: SIPTimers { configuration.timers }

    public private(set) var registration: SIPRegistrationState = .unregistered
    private var local: (host: String, port: UInt16)?
    private var receiveTask: Task<Void, Never>?
    private var refreshTask: Task<Void, Never>?
    private var isStopped = false

    private let registerCallID = SIPDigest.randomToken(12)
    private let registerTag = SIPDigest.randomToken(6)
    private var registerCSeq: UInt32 = 0

    /// Open client transactions, keyed by branch and CSeq method.
    private var transactions: [String: AsyncStream<SIPMessage>.Continuation] = [:]
    /// ACKs we sent for INVITE responses, by INVITE branch, to answer
    /// retransmitted final responses.
    private var sentAcks: [String: Data] = [:]
    /// Our final responses to INVITEs, retransmitted until the ACK.
    private var ackWaits: [String: Task<Void, any Error>] = [:]
    private var calls: [String: Dialog] = [:]
    private var sendChain: Task<Void, Never>?

    private struct Dialog {
        enum State: Equatable {
            case calling
            /// We hung up before the final response; CANCEL/BYE pending.
            case cancelling
            case incoming
            case confirmed
        }

        let id: String
        let isOutgoing: Bool
        var state: State
        let localTag: String
        var remoteTag: String?
        let localURI: String
        let remoteURI: String
        var remoteTarget: String
        var routeSet: [String] = []
        var localCSeq: UInt32 = 0
        let localRTPPort: UInt16
        /// Outgoing: the last INVITE sent (for CANCEL). Incoming: the INVITE.
        var invite: SIPMessage?
        var receivedProvisional = false
        var remoteMedia: SDPMedia?
    }

    public init(account: SIPAccount, transport: any SIPTransport, configuration: SIPUserAgentConfiguration = SIPUserAgentConfiguration()) {
        self.account = account
        self.transport = transport
        self.configuration = configuration
        (events, eventContinuation) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(100))
    }

    // MARK: - Lifecycle

    /// Starts listening and registers. Returns once the first REGISTER
    /// has an outcome.
    public func start() async {
        guard receiveTask == nil, !isStopped else { return }
        guard let endpoint = await transport.localEndpoint() else {
            setRegistration(.failed(.transport))
            return
        }
        local = endpoint
        receiveTask = Task { [transport] in
            for await datagram in transport.incoming {
                self.handle(datagram)
            }
            self.transportClosed()
        }
        await register()
    }

    /// Hangs up, unregisters (best effort) and closes the transport.
    public func stop() async {
        guard !isStopped else { return }
        isStopped = true
        refreshTask?.cancel()
        for id in Array(calls.keys) { hangUp(id) }
        if local != nil, case .registered = registration {
            _ = await sendAuthenticated { authorization in self.makeRegister(expires: 0, authorization: authorization) }
        }
        setRegistration(.unregistered)
        for task in ackWaits.values { task.cancel() }
        receiveTask?.cancel()
        transport.close()
        eventContinuation.finish()
    }

    private func transportClosed() {
        guard !isStopped else { return }
        for id in Array(calls.keys) { finish(id, .timeout) }
        setRegistration(.failed(.transport))
    }

    // MARK: - Registration

    private func register() async {
        guard !isStopped else { return }
        refreshTask?.cancel()
        // A refresh keeps showing "registered".
        if case .registered = registration {} else { setRegistration(.registering) }
        let response = await sendAuthenticated { authorization in
            self.makeRegister(expires: self.configuration.registerExpires, authorization: authorization)
        }
        guard !isStopped else { return }
        guard let response, let status = response.status else {
            setRegistration(.failed(.timeout))
            scheduleRegister(after: configuration.retryAfterFailure)
            return
        }
        switch status {
        case 200..<300:
            let expires = grantedExpires(in: response) ?? configuration.registerExpires
            setRegistration(.registered(expires: expires))
            // Renew well before expiry, at the latest after half the time.
            let seconds = max(min(expires - 30, expires / 2 + expires / 4), 5)
            scheduleRegister(after: .seconds(seconds))
        case 401, 403, 407:
            setRegistration(.failed(.authentication))
        default:
            setRegistration(.failed(.rejected(status)))
            scheduleRegister(after: configuration.retryAfterFailure)
        }
    }

    /// Registers again now, e.g. after returning to the home Wi-Fi.
    public func reregister() async {
        await register()
    }

    private func scheduleRegister(after delay: Duration) {
        refreshTask?.cancel()
        refreshTask = Task {
            try? await Task.sleep(for: delay)
            guard !Task.isCancelled else { return }
            await self.register()
        }
    }

    private func grantedExpires(in response: SIPMessage) -> Int? {
        let ours = response.headers.values("Contact").compactMap(SIPAddress.init(parsing:)).first { address in
            guard let local else { return false }
            return address.uri.contains("\(local.host):\(local.port)")
        }
        if let value = ours?.parameter("expires").flatMap(Int.init) { return value }
        return response.headers["Expires"].flatMap(Int.init)
    }

    private func makeRegister(expires: Int, authorization: SIPHeaders.Field?) -> SIPMessage {
        registerCSeq += 1
        let aor = SIPURI.make(user: account.username, host: account.registrar)
        var headers = baseHeaders()
        headers.add("From", "<\(aor)>;tag=\(registerTag)")
        headers.add("To", "<\(aor)>")
        headers.add("Call-ID", registerCallID)
        headers.add("CSeq", "\(registerCSeq) REGISTER")
        headers.add("Contact", "<\(contactURI)>;expires=\(expires)")
        headers.add("Expires", String(expires))
        if let authorization { headers.add(authorization.name, authorization.value) }
        return .request("REGISTER", uri: SIPURI.make(user: nil, host: account.registrar), headers: headers)
    }

    private func setRegistration(_ state: SIPRegistrationState) {
        guard registration != state else { return }
        registration = state
        eventContinuation.yield(.registration(state))
    }

    // MARK: - Outgoing calls

    /// Starts a call; progress arrives as events. Returns the Call-ID.
    public func call(_ number: String) throws(SIPFailure) -> String {
        guard let local, !isStopped else { throw .transport }
        let id = "\(SIPDigest.randomToken(12))@\(local.host)"
        let dialog = Dialog(
            id: id,
            isOutgoing: true,
            state: .calling,
            localTag: SIPDigest.randomToken(6),
            localURI: SIPURI.make(user: account.username, host: account.registrar),
            remoteURI: SIPURI.make(user: number, host: account.registrar),
            remoteTarget: SIPURI.make(user: number, host: account.registrar),
            localRTPPort: configuration.rtpPort()
        )
        calls[id] = dialog
        Task { await self.runOutgoing(id) }
        return id
    }

    private func runOutgoing(_ id: String) async {
        let response = await sendAuthenticated(
            { authorization in self.makeInvite(id, authorization: authorization) },
            onProvisional: { response in self.outgoingProgress(id, response) }
        )
        guard var dialog = calls[id] else { return }
        guard let response, let status = response.status else {
            finish(id, .timeout)
            return
        }
        guard (200..<300).contains(status) else {
            // The transaction sent the ACK.
            if dialog.state == .cancelling {
                finish(id, .localHangUp, emit: false)
            } else {
                finish(id, Self.endReason(for: status))
            }
            return
        }

        dialog.remoteTag = response.to?.tag
        if let target = response.contact?.uri { dialog.remoteTarget = target }
        dialog.routeSet = response.headers.values("Record-Route").reversed()
        calls[id] = dialog
        acknowledge(response, dialog: dialog)

        if dialog.state == .cancelling {
            // Answered while we were hanging up: end it properly.
            sendBye(dialog)
            finish(id, .localHangUp, emit: false)
            return
        }
        let media = (try? SDP.negotiate(response.body)) ?? dialog.remoteMedia
        guard let media else {
            sendBye(dialog)
            finish(id, .noCommonCodec)
            return
        }
        dialog.remoteMedia = media
        dialog.state = .confirmed
        calls[id] = dialog
        eventContinuation.yield(.connected(callID: id, media: media, localRTPPort: dialog.localRTPPort))
    }

    private func outgoingProgress(_ id: String, _ response: SIPMessage) {
        guard var dialog = calls[id], let status = response.status, status > 100 else { return }
        let firstProvisional = !dialog.receivedProvisional
        dialog.receivedProvisional = true
        let earlyMedia = response.body.isEmpty ? nil : try? SDP.negotiate(response.body)
        if let earlyMedia { dialog.remoteMedia = earlyMedia }
        calls[id] = dialog
        if dialog.state == .cancelling {
            if firstProvisional { sendCancel(dialog) }
            return
        }
        eventContinuation.yield(.ringing(callID: id, earlyMedia: earlyMedia, localRTPPort: dialog.localRTPPort))
    }

    private func makeInvite(_ id: String, authorization: SIPHeaders.Field?) -> SIPMessage {
        guard var dialog = calls[id], let local else {
            return .request("INVITE", uri: "sip:invalid")
        }
        dialog.localCSeq += 1
        var headers = baseHeaders()
        headers.add("From", "<\(dialog.localURI)>;tag=\(dialog.localTag)")
        headers.add("To", "<\(dialog.remoteURI)>")
        headers.add("Call-ID", id)
        headers.add("CSeq", "\(dialog.localCSeq) INVITE")
        headers.add("Contact", "<\(contactURI)>")
        headers.add("Content-Type", "application/sdp")
        if let authorization { headers.add(authorization.name, authorization.value) }
        let body = SDP.make(address: local.host, port: dialog.localRTPPort)
        let invite = SIPMessage.request("INVITE", uri: dialog.remoteTarget, headers: headers, body: body)
        dialog.invite = invite
        calls[id] = dialog
        return invite
    }

    static func endReason(for status: Int) -> SIPCallEndReason {
        switch status {
        case 486, 600: .busy
        case 603: .declined
        case 404, 484, 604: .notFound
        case 408, 480, 487: .unanswered
        case 488, 606: .noCommonCodec
        default: .failed(status)
        }
    }

    /// ACK for a 2xx: a request of its own, within the dialog (§13.2.2.4).
    private func acknowledge(_ response: SIPMessage, dialog: SIPUserAgent.Dialog) {
        guard let branch = response.topViaBranch, let cseq = response.cseq else { return }
        var headers = baseHeaders()
        headers.add("From", "<\(dialog.localURI)>;tag=\(dialog.localTag)")
        headers["To"] = response.headers["To"]
        headers.add("Call-ID", dialog.id)
        headers.add("CSeq", "\(cseq.number) ACK")
        for route in dialog.routeSet { headers.add("Route", route) }
        let ack = SIPMessage.request("ACK", uri: dialog.remoteTarget, headers: headers).serialized()
        remember(ack: ack, forInviteBranch: branch)
        send(ack)
    }

    private func remember(ack: Data, forInviteBranch branch: String) {
        sentAcks[branch] = ack
        Task {
            try? await Task.sleep(for: self.timers.timeout)
            self.forgetAck(branch)
        }
    }

    private func forgetAck(_ branch: String) {
        sentAcks[branch] = nil
    }

    // MARK: - Incoming calls

    /// Answers a ringing incoming call with PCMA.
    public func answer(_ id: String) -> Bool {
        guard var dialog = calls[id], dialog.state == .incoming, let invite = dialog.invite,
              let media = dialog.remoteMedia, let local
        else { return false }
        let sdp = SDP.make(address: local.host, port: dialog.localRTPPort, codecs: [media.codec], telephoneEvent: media.telephoneEventPayloadType)
        let ok = response(to: invite, status: 200, reason: "OK", toTag: dialog.localTag, contact: true, body: sdp)
        dialog.state = .confirmed
        calls[id] = dialog
        retransmitUntilAck(id, ok.serialized(), dialogEstablished: true)
        eventContinuation.yield(.connected(callID: id, media: media, localRTPPort: dialog.localRTPPort))
        return true
    }

    /// Declines a ringing incoming call: 486 Busy Here or 603 Decline.
    public func reject(_ id: String, busy: Bool = false) {
        guard let dialog = calls[id], dialog.state == .incoming, let invite = dialog.invite else { return }
        let final = response(to: invite, status: busy ? 486 : 603, reason: busy ? "Busy Here" : "Decline", toTag: dialog.localTag)
        retransmitUntilAck(id, final.serialized(), dialogEstablished: false)
        finish(id, busy ? .busy : .declined, emit: false)
    }

    private func handleInvite(_ request: SIPMessage) {
        guard let id = request.callID else { return }
        if var dialog = calls[id] {
            if request.to?.tag == nil {
                // Retransmission while ringing: repeat the provisional answer.
                if dialog.state == .incoming, let invite = dialog.invite {
                    send(response(to: invite, status: 180, reason: "Ringing", toTag: dialog.localTag, contact: true))
                }
                return
            }
            // re-INVITE (e.g. hold from the other side): keep our media.
            guard dialog.state == .confirmed, let local else { return }
            if let media = try? SDP.negotiate(request.body) { dialog.remoteMedia = media }
            if let target = request.contact?.uri { dialog.remoteTarget = target }
            calls[id] = dialog
            let codec = dialog.remoteMedia?.codec ?? .pcma
            let sdp = SDP.make(address: local.host, port: dialog.localRTPPort, codecs: [codec], telephoneEvent: dialog.remoteMedia?.telephoneEventPayloadType, version: 2)
            let ok = response(to: request, status: 200, reason: "OK", toTag: dialog.localTag, contact: true, body: sdp)
            retransmitUntilAck(id, ok.serialized(), dialogEstablished: false)
            return
        }
        guard request.to?.tag == nil else {
            send(response(to: request, status: 481, reason: "Call/Transaction Does Not Exist"))
            return
        }
        let localTag = SIPDigest.randomToken(6)
        guard !calls.values.contains(where: { $0.state != .cancelling }) else {
            let busy = response(to: request, status: 486, reason: "Busy Here", toTag: localTag)
            retransmitUntilAck(id, busy.serialized(), dialogEstablished: false)
            return
        }
        let media: SDPMedia
        do {
            media = try SDP.negotiate(request.body)
        } catch {
            let refusal = response(to: request, status: 488, reason: "Not Acceptable Here", toTag: localTag)
            retransmitUntilAck(id, refusal.serialized(), dialogEstablished: false)
            return
        }
        send(response(to: request, status: 100, reason: "Trying"))

        let from = request.from
        let dialog = Dialog(
            id: id,
            isOutgoing: false,
            state: .incoming,
            localTag: localTag,
            remoteTag: from?.tag,
            localURI: request.to?.uri ?? SIPURI.make(user: account.username, host: account.registrar),
            remoteURI: from?.uri ?? "",
            remoteTarget: request.contact?.uri ?? from?.uri ?? "",
            routeSet: request.headers.values("Record-Route"),
            localRTPPort: configuration.rtpPort(),
            invite: request,
            remoteMedia: media
        )
        calls[id] = dialog
        send(response(to: request, status: 180, reason: "Ringing", toTag: localTag, contact: true))
        let user = from?.user ?? ""
        let number = user.lowercased() == "anonymous" || !user.contains(where: \.isNumber) ? "" : user
        eventContinuation.yield(.incoming(SIPIncomingCall(id: id, number: number, displayName: from?.displayName)))
    }

    private func handleCancel(_ request: SIPMessage) {
        guard let id = request.callID else { return }
        guard let dialog = calls[id], dialog.state == .incoming, let invite = dialog.invite else {
            send(response(to: request, status: 481, reason: "Call/Transaction Does Not Exist"))
            return
        }
        send(response(to: request, status: 200, reason: "OK", toTag: dialog.localTag))
        let terminated = response(to: invite, status: 487, reason: "Request Terminated", toTag: dialog.localTag)
        retransmitUntilAck(id, terminated.serialized(), dialogEstablished: false)
        finish(id, .remoteCancelled)
    }

    private func handleBye(_ request: SIPMessage) {
        guard let id = request.callID, calls[id] != nil else {
            send(response(to: request, status: 481, reason: "Call/Transaction Does Not Exist"))
            return
        }
        send(response(to: request, status: 200, reason: "OK"))
        finish(id, .remoteHangUp)
    }

    /// Our final response to an INVITE until the ACK arrives (Timer G/H).
    private func retransmitUntilAck(_ id: String, _ data: Data, dialogEstablished: Bool) {
        ackWaits[id]?.cancel()
        ackWaits[id] = Task {
            var interval = self.timers.t1
            var elapsed = Duration.zero
            self.send(data)
            while elapsed < self.timers.timeout {
                try await Task.sleep(for: interval)
                elapsed += interval
                self.send(data)
                interval = min(interval * 2, self.timers.t2)
            }
            self.ackMissing(id, dialogEstablished: dialogEstablished)
        }
    }

    private func ackMissing(_ id: String, dialogEstablished: Bool) {
        ackWaits[id] = nil
        guard dialogEstablished, let dialog = calls[id] else { return }
        sendBye(dialog)
        finish(id, .timeout)
    }

    // MARK: - Hanging up

    /// Ends a call in any state: CANCEL or BYE for our calls, 603 for a
    /// ringing incoming one, BYE for a connected call.
    public func hangUp(_ id: String) {
        guard var dialog = calls[id] else { return }
        switch dialog.state {
        case .incoming:
            reject(id)
            eventContinuation.yield(.ended(callID: id, reason: .localHangUp))
        case .calling:
            dialog.state = .cancelling
            calls[id] = dialog
            // CANCEL only after a provisional response (§9.1); otherwise
            // `outgoingProgress` sends it.
            if dialog.receivedProvisional { sendCancel(dialog) }
            eventContinuation.yield(.ended(callID: id, reason: .localHangUp))
        case .cancelling:
            break
        case .confirmed:
            sendBye(dialog)
            finish(id, .localHangUp)
        }
    }

    private func sendCancel(_ dialog: Dialog) {
        guard let invite = dialog.invite, let cseq = invite.cseq else { return }
        var headers = SIPHeaders()
        for via in invite.headers.rawValues("Via").prefix(1) { headers.add("Via", via) }
        headers.add("Max-Forwards", "70")
        headers["From"] = invite.headers["From"]
        headers["To"] = invite.headers["To"]
        headers.add("Call-ID", dialog.id)
        headers.add("CSeq", "\(cseq.number) CANCEL")
        headers.add("User-Agent", configuration.userAgent)
        let cancel = SIPMessage.request("CANCEL", uri: invite.requestURI ?? dialog.remoteTarget, headers: headers)
        Task { _ = await self.transact(cancel) }
    }

    private func sendBye(_ dialog: Dialog) {
        let id = dialog.id
        Task {
            _ = await self.sendAuthenticated { authorization in
                self.makeInDialog("BYE", id: id, fallback: dialog, authorization: authorization)
            }
        }
    }

    private func makeInDialog(_ method: String, id: String, fallback: Dialog, authorization: SIPHeaders.Field?) -> SIPMessage {
        var dialog = calls[id] ?? fallback
        dialog.localCSeq += 1
        if calls[id] != nil { calls[id] = dialog }
        var headers = baseHeaders()
        let localTag = ";tag=\(dialog.localTag)"
        let remoteTag = dialog.remoteTag.map { ";tag=\($0)" } ?? ""
        headers.add("From", "<\(dialog.localURI)>\(localTag)")
        headers.add("To", "<\(dialog.remoteURI)>\(remoteTag)")
        headers.add("Call-ID", id)
        headers.add("CSeq", "\(dialog.localCSeq) \(method)")
        for route in dialog.routeSet { headers.add("Route", route) }
        if let authorization { headers.add(authorization.name, authorization.value) }
        return .request(method, uri: dialog.remoteTarget, headers: headers)
    }

    private func finish(_ id: String, _ reason: SIPCallEndReason, emit: Bool = true) {
        guard calls.removeValue(forKey: id) != nil else { return }
        if emit { eventContinuation.yield(.ended(callID: id, reason: reason)) }
    }

    // MARK: - Receiving

    private func handle(_ datagram: Data) {
        guard let message = try? SIPMessage(parsing: datagram) else { return }
        guard let method = message.method else {
            handleResponse(message)
            return
        }
        switch method {
        case "INVITE": handleInvite(message)
        case "ACK":
            if let id = message.callID { ackWaits.removeValue(forKey: id)?.cancel() }
        case "CANCEL": handleCancel(message)
        case "BYE": handleBye(message)
        case "OPTIONS", "NOTIFY", "INFO":
            send(response(to: message, status: 200, reason: "OK"))
        default:
            send(response(to: message, status: 501, reason: "Not Implemented"))
        }
    }

    private func handleResponse(_ response: SIPMessage) {
        guard let branch = response.topViaBranch, let cseq = response.cseq else { return }
        if let transaction = transactions["\(branch)|\(cseq.method)"] {
            transaction.yield(response)
        } else if cseq.method == "INVITE", let status = response.status, status >= 200, let ack = sentAcks[branch] {
            // Retransmitted final response: our ACK got lost.
            send(ack)
        }
    }

    // MARK: - Client transactions

    /// Sends a request and repeats it with digest credentials when
    /// challenged (at most twice, so a wrong password fails fast).
    private func sendAuthenticated(
        _ makeRequest: (SIPHeaders.Field?) -> SIPMessage,
        onProvisional: (SIPMessage) -> Void = { _ in }
    ) async -> SIPMessage? {
        var authorization: SIPHeaders.Field?
        for attempt in 0..<3 {
            let request = makeRequest(authorization)
            guard let response = await transact(request, onProvisional: onProvisional) else { return nil }
            guard let status = response.status, status == 401 || status == 407, attempt < 2,
                  let method = request.method, let uri = request.requestURI
            else { return response }
            let headerName = status == 401 ? "WWW-Authenticate" : "Proxy-Authenticate"
            guard let challenge = response.headers.rawValues(headerName).lazy.compactMap(SIPDigestChallenge.init(header:)).first(where: \.supportsMD5),
                  attempt == 0 || challenge.stale
            else { return response }
            authorization = SIPHeaders.Field(
                name: status == 401 ? "Authorization" : "Proxy-Authorization",
                value: SIPDigest.authorization(challenge: challenge, method: method, uri: uri, username: account.username, password: account.password)
            )
        }
        return nil
    }

    /// One client transaction: retransmits over UDP until a response
    /// (INVITE: until a provisional one), gives up after 64·T1, ACKs
    /// non-2xx final responses to INVITE. Returns the final response.
    private func transact(_ request: SIPMessage, onProvisional: (SIPMessage) -> Void = { _ in }) async -> SIPMessage? {
        guard let branch = request.topViaBranch, let method = request.cseq?.method else { return nil }
        let key = "\(branch)|\(method)"
        let (responses, continuation) = AsyncStream<SIPMessage>.makeStream()
        transactions[key] = continuation
        let data = request.serialized()
        let isInvite = method == "INVITE"
        let timers = timers

        let retransmit = Task {
            var interval = timers.t1
            self.send(data)
            while !Task.isCancelled {
                try await Task.sleep(for: interval)
                self.send(data)
                interval = isInvite ? interval * 2 : min(interval * 2, timers.t2)
            }
        }
        let timeout = Task {
            try await Task.sleep(for: timers.timeout)
            continuation.finish()
        }
        defer {
            retransmit.cancel()
            timeout.cancel()
            transactions[key] = nil
        }

        for await response in responses {
            guard let status = response.status else { continue }
            if status < 200 {
                if isInvite {
                    // Proceeding: the callee may ring for minutes.
                    retransmit.cancel()
                    timeout.cancel()
                }
                onProvisional(response)
                continue
            }
            if isInvite, status >= 300 {
                let ack = nonSuccessAck(for: request, response: response)
                remember(ack: ack, forInviteBranch: branch)
                send(ack)
            }
            return response
        }
        return nil
    }

    /// ACK within the INVITE transaction (§17.1.1.3).
    private func nonSuccessAck(for invite: SIPMessage, response: SIPMessage) -> Data {
        var headers = SIPHeaders()
        for via in invite.headers.rawValues("Via").prefix(1) { headers.add("Via", via) }
        headers.add("Max-Forwards", "70")
        headers["From"] = invite.headers["From"]
        headers["To"] = response.headers["To"]
        headers["Call-ID"] = invite.headers["Call-ID"]
        headers.add("CSeq", "\(invite.cseq?.number ?? 1) ACK")
        for route in invite.headers.rawValues("Route") { headers.add("Route", route) }
        return SIPMessage.request("ACK", uri: invite.requestURI ?? "", headers: headers).serialized()
    }

    // MARK: - Building messages

    private var contactURI: String {
        SIPURI.make(user: account.username, host: local?.host ?? "0.0.0.0", port: local?.port)
    }

    private func baseHeaders() -> SIPHeaders {
        var headers = SIPHeaders()
        let host = local.map { SIPURI.make(user: nil, host: $0.host, port: $0.port).dropFirst(4) } ?? "0.0.0.0"
        headers.add("Via", "SIP/2.0/UDP \(host);branch=z9hG4bK\(SIPDigest.randomToken(10));rport")
        headers.add("Max-Forwards", "70")
        headers.add("User-Agent", configuration.userAgent)
        headers.add("Allow", Self.allow)
        return headers
    }

    private static let allow = "INVITE, ACK, CANCEL, BYE, OPTIONS, NOTIFY, INFO"

    private func response(to request: SIPMessage, status: Int, reason: String, toTag: String? = nil, contact: Bool = false, body: Data = Data()) -> SIPMessage {
        var headers = SIPHeaders()
        for via in request.headers.rawValues("Via") { headers.add("Via", via) }
        for route in request.headers.rawValues("Record-Route") where (100..<300).contains(status) && request.method == "INVITE" {
            headers.add("Record-Route", route)
        }
        headers["From"] = request.headers["From"]
        if var to = request.to, let toTag, to.tag == nil, status > 100 {
            to.tag = toTag
            headers.add("To", to.headerValue)
        } else {
            headers["To"] = request.headers["To"]
        }
        headers["Call-ID"] = request.headers["Call-ID"]
        headers["CSeq"] = request.headers["CSeq"]
        if contact { headers.add("Contact", "<\(contactURI)>") }
        headers.add("User-Agent", configuration.userAgent)
        if status == 200 || status == 405 || status == 501 { headers.add("Allow", Self.allow) }
        if !body.isEmpty { headers.add("Content-Type", "application/sdp") }
        return SIPMessage(startLine: .response(status: status, reason: reason), headers: headers, body: body)
    }

    private func send(_ message: SIPMessage) {
        send(message.serialized())
    }

    /// Sends in call order, without waiting.
    private func send(_ data: Data) {
        let transport = transport
        let previous = sendChain
        sendChain = Task {
            await previous?.value
            try? await transport.send(data)
        }
    }
}
