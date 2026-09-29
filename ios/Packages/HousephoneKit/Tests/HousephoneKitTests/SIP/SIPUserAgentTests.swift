import Foundation
import Testing
@testable import HousephoneKit

/// Full flows against a scripted FRITZ!Box, with T1 = 10 ms.
struct SIPUserAgentTests {
    static let password = "test-password"

    static func makeAgent(_ transport: FakeSIPTransport, registerExpires: Int = 600) -> SIPUserAgent {
        var configuration = SIPUserAgentConfiguration()
        configuration.timers = SIPTimers(t1: .milliseconds(10), t2: .milliseconds(40))
        configuration.retryAfterFailure = .seconds(60)
        configuration.registerExpires = registerExpires
        configuration.rtpPort = { 20_000 }
        return SIPUserAgent(account: SIPAccount(registrar: FakeFritzBox.host, username: "620", password: password), transport: transport, configuration: configuration)
    }

    /// Starts the agent and completes a challenged registration.
    static func registered(_ transport: FakeSIPTransport) async throws -> (SIPUserAgent, EventRecorder) {
        let agent = makeAgent(transport)
        let events = EventRecorder(agent.events)
        async let started: Void = agent.start()
        let first = try await transport.nextRequest("REGISTER")
        transport.deliver(FakeFritzBox.challenge(first))
        let second = try await transport.nextRequest("REGISTER")
        #expect(FakeFritzBox.isAuthorized(second, password: password))
        transport.deliver(FakeFritzBox.response(to: second, 200, "OK", headers: [("Contact", "<sip:620@192.0.2.10:5062>;expires=300")]))
        await started
        return (agent, events)
    }

    // MARK: - Registration

    @Test func registersWithDigestAuth() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)
        #expect(await agent.registration == .registered(expires: 300))
        _ = try await events.next("registered") { $0 == .registration(.registered(expires: 300)) }

        let register = transport.allSent.first { $0.method == "REGISTER" }!
        #expect(register.requestURI == "sip:192.0.2.1")
        #expect(register.from?.uri == "sip:620@192.0.2.1")
        #expect(register.contact?.uri == "sip:620@192.0.2.10:5062")
        #expect(register.headers["Via"]?.hasPrefix("SIP/2.0/UDP 192.0.2.10:5062;branch=z9hG4bK") == true)
        let registers = transport.allSent.filter { $0.method == "REGISTER" }
        #expect(registers[0].cseq!.number + 1 == registers[1].cseq!.number)
        #expect(registers[0].topViaBranch != registers[1].topViaBranch)
        #expect(Set(registers.map(\.callID)).count == 1)
        await agent.stop()
    }

    @Test func wrongPasswordFailsWithoutRetrying() async throws {
        let transport = FakeSIPTransport()
        let agent = Self.makeAgent(transport)
        async let started: Void = agent.start()
        let first = try await transport.nextRequest("REGISTER")
        transport.deliver(FakeFritzBox.challenge(first))
        let second = try await transport.nextRequest("REGISTER")
        transport.deliver(FakeFritzBox.challenge(second, nonce: "fresh"))
        await started
        #expect(await agent.registration == .failed(.authentication))
        try await Task.sleep(for: .milliseconds(100))
        #expect(transport.count { $0.method == "REGISTER" } == 2)
        await agent.stop()
    }

    @Test func retransmitsThenTimesOut() async throws {
        let transport = FakeSIPTransport()
        let agent = Self.makeAgent(transport)
        let begin = ContinuousClock.now
        await agent.start()
        let elapsed = ContinuousClock.now - begin
        #expect(await agent.registration == .failed(.timeout))
        // Timer E: 10, 20, 40, 40, … ms until Timer F at 640 ms.
        let count = transport.count { $0.method == "REGISTER" }
        #expect((10...20).contains(count), "sent \(count) times")
        #expect(elapsed >= .milliseconds(640))
        // Every retransmission is the same request.
        #expect(Set(transport.allSent.map(\.topViaBranch)).count == 1)
        await agent.stop()
    }

    @Test func noNetworkFailsRightAway() async {
        let agent = Self.makeAgent(FakeSIPTransport(endpoint: nil))
        await agent.start()
        #expect(await agent.registration == .failed(.transport))
        await #expect(throws: SIPFailure.transport) { try await agent.call("5550100") }
    }

    @Test func refreshesRegistrationBeforeExpiry() async throws {
        let transport = FakeSIPTransport()
        let agent = Self.makeAgent(transport)
        async let started: Void = agent.start()
        let first = try await transport.nextRequest("REGISTER")
        // Granted 6 s: the refresh comes after at most 4.5 s.
        transport.deliver(FakeFritzBox.response(to: first, 200, "OK", headers: [("Expires", "6")]))
        await started
        #expect(await agent.registration == .registered(expires: 6))
        let refresh = try await transport.nextRequest("REGISTER", timeout: .seconds(6))
        #expect(refresh.cseq!.number == first.cseq!.number + 1)
        await agent.stop()
    }

    @Test func unregistersOnStop() async throws {
        let transport = FakeSIPTransport()
        let (agent, _) = try await Self.registered(transport)
        async let stopped: Void = agent.stop()
        let unregister = try await transport.next("REGISTER expires=0") { $0.method == "REGISTER" && $0.headers["Expires"] == "0" }
        transport.deliver(FakeFritzBox.response(to: unregister, 200, "OK"))
        await stopped
        #expect(await agent.registration == .unregistered)
    }

    // MARK: - Outgoing

    @Test func outgoingCallWithProxyAuthRingingAnswerAndHangUp() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)

        let id = try await agent.call("*31#5550100")
        let invite1 = try await transport.nextRequest("INVITE")
        #expect(invite1.requestURI == "sip:*31%235550100@192.0.2.1")
        #expect(invite1.callID == id)
        let offer = try SDP.negotiate(invite1.body)
        #expect(offer.address == "192.0.2.10" && offer.port == 20_000)

        transport.deliver(FakeFritzBox.challenge(invite1, status: 407))
        let ack1 = try await transport.nextRequest("ACK")
        #expect(ack1.topViaBranch == invite1.topViaBranch)
        #expect(ack1.cseq?.number == invite1.cseq?.number)

        let invite2 = try await transport.nextRequest("INVITE")
        #expect(FakeFritzBox.isAuthorized(invite2, header: "Proxy-Authorization", password: Self.password))
        #expect(invite2.cseq!.number == invite1.cseq!.number + 1)
        #expect(invite2.from?.tag == invite1.from?.tag)

        transport.deliver(FakeFritzBox.response(to: invite2, 100, "Trying"))
        transport.deliver(FakeFritzBox.response(to: invite2, 180, "Ringing", toTag: "fb1"))
        _ = try await events.next("ringing") {
            if case .ringing(id, nil, 20_000) = $0 { true } else { false }
        }
        // Proceeding: no more retransmissions of the INVITE.
        try await Task.sleep(for: .milliseconds(100))
        let invites = transport.count { $0.method == "INVITE" && $0.topViaBranch == invite2.topViaBranch }

        let answer = FakeFritzBox.response(to: invite2, 200, "OK", toTag: "fb1", headers: [
            ("Contact", "<sip:5550100@192.0.2.1:5060>"),
            ("Record-Route", "<sip:192.0.2.1;lr>"),
        ], body: Data(FakeFritzBox.offer.utf8))
        transport.deliver(answer)
        let connected = try await events.next("connected") { if case .connected = $0 { true } else { false } }
        #expect(connected == .connected(callID: id, media: try SDP.negotiate(Data(FakeFritzBox.offer.utf8)), localRTPPort: 20_000))

        let ack2 = try await transport.nextRequest("ACK")
        #expect(ack2.requestURI == "sip:5550100@192.0.2.1:5060")
        #expect(ack2.to?.tag == "fb1")
        #expect(ack2.headers["Route"] == "<sip:192.0.2.1;lr>")
        #expect(ack2.topViaBranch != invite2.topViaBranch)
        #expect(transport.count { $0.method == "INVITE" && $0.topViaBranch == invite2.topViaBranch } == invites)

        // A retransmitted 200 is ACKed again.
        transport.deliver(answer)
        _ = try await transport.nextRequest("ACK")

        await agent.hangUp(id)
        let bye = try await transport.nextRequest("BYE")
        #expect(bye.requestURI == "sip:5550100@192.0.2.1:5060")
        #expect(bye.from?.tag == invite1.from?.tag)
        #expect(bye.to?.tag == "fb1")
        #expect(bye.cseq!.number > invite2.cseq!.number)
        transport.deliver(FakeFritzBox.response(to: bye, 200, "OK"))
        _ = try await events.next("ended") { $0 == .ended(callID: id, reason: .localHangUp) }
        await agent.stop()
    }

    @Test(arguments: [(486, SIPCallEndReason.busy), (603, .declined), (404, .notFound), (480, .unanswered), (500, .failed(500))])
    func outgoingCallRejected(status: Int, reason: SIPCallEndReason) async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)
        let id = try await agent.call("5550100")
        let invite = try await transport.nextRequest("INVITE")
        transport.deliver(FakeFritzBox.response(to: invite, status, "No", toTag: "x"))
        _ = try await events.next("ended") { $0 == .ended(callID: id, reason: reason) }
        let ack = try await transport.nextRequest("ACK")
        #expect(ack.to?.tag == "x")
        await agent.stop()
    }

    @Test func hangingUpBeforeRingingCancelsAtTheFirstProvisional() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)
        let id = try await agent.call("5550100")
        let invite = try await transport.nextRequest("INVITE")
        await agent.hangUp(id)
        _ = try await events.next("ended") { $0 == .ended(callID: id, reason: .localHangUp) }
        try await Task.sleep(for: .milliseconds(30))
        #expect(transport.count { $0.method == "CANCEL" } == 0)

        transport.deliver(FakeFritzBox.response(to: invite, 180, "Ringing", toTag: "fb"))
        let cancel = try await transport.nextRequest("CANCEL")
        #expect(cancel.topViaBranch == invite.topViaBranch)
        #expect(cancel.cseq?.number == invite.cseq?.number)
        #expect(cancel.requestURI == invite.requestURI)
        transport.deliver(FakeFritzBox.response(to: cancel, 200, "OK"))
        transport.deliver(FakeFritzBox.response(to: invite, 487, "Request Terminated", toTag: "fb"))
        _ = try await transport.nextRequest("ACK")
        try await Task.sleep(for: .milliseconds(30))
        #expect(events.all.filter { if case .ringing = $0 { true } else { false } }.isEmpty)
        #expect(events.all.filter { if case .ended = $0 { true } else { false } }.count == 1)
        await agent.stop()
    }

    @Test func answeredWhileCancellingIsHungUp() async throws {
        let transport = FakeSIPTransport()
        let (agent, _) = try await Self.registered(transport)
        let id = try await agent.call("5550100")
        let invite = try await transport.nextRequest("INVITE")
        transport.deliver(FakeFritzBox.response(to: invite, 180, "Ringing", toTag: "fb"))
        try await Task.sleep(for: .milliseconds(20))
        await agent.hangUp(id)
        _ = try await transport.nextRequest("CANCEL")
        // The 200 crossed our CANCEL.
        transport.deliver(FakeFritzBox.response(to: invite, 200, "OK", toTag: "fb", headers: [("Contact", "<sip:5550100@192.0.2.1>")], body: Data(FakeFritzBox.offer.utf8)))
        _ = try await transport.nextRequest("ACK")
        let bye = try await transport.nextRequest("BYE")
        #expect(bye.to?.tag == "fb")
        await agent.stop()
    }

    // MARK: - Incoming

    @Test func incomingCallAnsweredAndHungUpRemotely() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)

        let invite = FakeFritzBox.invite()
        transport.deliver(invite)
        _ = try await transport.nextResponse(100, method: "INVITE")
        let ringing = try await transport.nextResponse(180, method: "INVITE")
        let ourTag = try #require(ringing.to?.tag)
        let incoming = try await events.next("incoming") { if case .incoming = $0 { true } else { false } }
        #expect(incoming == .incoming(SIPIncomingCall(id: invite.callID!, number: "5550100", displayName: "Test Anrufer")))

        // A retransmitted INVITE gets the 180 again, no second call.
        transport.deliver(invite)
        _ = try await transport.nextResponse(180, method: "INVITE")

        #expect(await agent.answer(invite.callID!))
        let ok = try await transport.nextResponse(200, method: "INVITE")
        #expect(ok.to?.tag == ourTag)
        #expect(ok.contact?.uri == "sip:620@192.0.2.10:5062")
        let answer = try SDP.negotiate(ok.body)
        #expect(answer.port == 20_000 && answer.codec == .pcma && answer.telephoneEventPayloadType == 101)
        _ = try await events.next("connected") { if case .connected(_, let media, 20_000) = $0 { media.port == 7078 } else { false } }

        // 200 is repeated until the ACK arrives.
        _ = try await transport.nextResponse(200, method: "INVITE")
        transport.deliver(FakeFritzBox.inDialog("ACK", invite: invite, ourTag: ourTag, cseq: 1))
        try await Task.sleep(for: .milliseconds(50))
        let settled = transport.count { $0.status == 200 && $0.cseq?.method == "INVITE" }
        try await Task.sleep(for: .milliseconds(100))
        #expect(transport.count { $0.status == 200 && $0.cseq?.method == "INVITE" } == settled)

        transport.deliver(FakeFritzBox.inDialog("BYE", invite: invite, ourTag: ourTag))
        _ = try await transport.nextResponse(200, method: "BYE")
        _ = try await events.next("ended") { $0 == .ended(callID: invite.callID!, reason: .remoteHangUp) }
        await agent.stop()
    }

    @Test func missingAckEndsTheCall() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)
        let invite = FakeFritzBox.invite()
        transport.deliver(invite)
        _ = try await events.next("incoming") { if case .incoming = $0 { true } else { false } }
        #expect(await agent.answer(invite.callID!))
        _ = try await transport.nextRequest("BYE")
        _ = try await events.next("ended") { $0 == .ended(callID: invite.callID!, reason: .timeout) }
        await agent.stop()
    }

    @Test func callerCancels() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)
        let invite = FakeFritzBox.invite(name: nil)
        transport.deliver(invite)
        let ringing = try await transport.nextResponse(180, method: "INVITE")
        _ = try await events.next("incoming") { $0 == .incoming(SIPIncomingCall(id: invite.callID!, number: "5550100", displayName: nil)) }

        var cancel = FakeFritzBox.inDialog("CANCEL", invite: invite, ourTag: nil, cseq: 1)
        cancel.headers["Via"] = invite.headers["Via"]
        transport.deliver(cancel)
        _ = try await transport.nextResponse(200, method: "CANCEL")
        let terminated = try await transport.nextResponse(487, method: "INVITE")
        #expect(terminated.to?.tag == ringing.to?.tag)
        _ = try await events.next("ended") { $0 == .ended(callID: invite.callID!, reason: .remoteCancelled) }
        #expect(await agent.answer(invite.callID!) == false)
        await agent.stop()
    }

    @Test func declineAndBusy() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)
        let first = FakeFritzBox.invite(callID: "a@192.0.2.1")
        transport.deliver(first)
        _ = try await events.next("incoming") { if case .incoming = $0 { true } else { false } }

        // One call at a time.
        transport.deliver(FakeFritzBox.invite(callID: "b@192.0.2.1"))
        let busy = try await transport.nextResponse(486, method: "INVITE")
        #expect(busy.callID == "b@192.0.2.1")

        await agent.hangUp(first.callID!)
        let decline = try await transport.nextResponse(603, method: "INVITE")
        #expect(decline.callID == "a@192.0.2.1")
        _ = try await events.next("ended") { $0 == .ended(callID: "a@192.0.2.1", reason: .localHangUp) }
        await agent.stop()
    }

    @Test func withheldNumberAndUnusableOffer() async throws {
        let transport = FakeSIPTransport()
        let (agent, events) = try await Self.registered(transport)
        transport.deliver(FakeFritzBox.invite(callID: "g722@192.0.2.1", body: "v=0\r\nc=IN IP4 192.0.2.1\r\nm=audio 7078 RTP/AVP 9\r\na=rtpmap:9 G722/8000\r\n"))
        _ = try await transport.nextResponse(488, method: "INVITE")

        transport.deliver(FakeFritzBox.invite(callID: "anon@192.0.2.1", from: "anonymous", name: "Anonymous"))
        _ = try await events.next("incoming") { $0 == .incoming(SIPIncomingCall(id: "anon@192.0.2.1", number: "", displayName: "Anonymous")) }
        await agent.stop()
    }

    @Test func answersOptionsAndRejectsUnknownMethods() async throws {
        let transport = FakeSIPTransport()
        let (agent, _) = try await Self.registered(transport)
        let options = FakeFritzBox.inDialog("OPTIONS", invite: FakeFritzBox.invite(callID: "o@192.0.2.1"), ourTag: nil, cseq: 1)
        transport.deliver(options)
        let ok = try await transport.nextResponse(200, method: "OPTIONS")
        #expect(ok.headers["Allow"]?.contains("INVITE") == true)
        transport.deliver(FakeFritzBox.inDialog("SUBSCRIBE", invite: FakeFritzBox.invite(callID: "s@192.0.2.1"), ourTag: nil, cseq: 1))
        _ = try await transport.nextResponse(501, method: "SUBSCRIBE")
        transport.deliver(FakeFritzBox.inDialog("BYE", invite: FakeFritzBox.invite(callID: "nope@192.0.2.1"), ourTag: "x"))
        _ = try await transport.nextResponse(481, method: "BYE")
        await agent.stop()
    }
}
