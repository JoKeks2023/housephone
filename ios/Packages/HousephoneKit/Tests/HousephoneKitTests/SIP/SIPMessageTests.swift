import Foundation
import Testing
@testable import HousephoneKit

struct SIPMessageTests {
    /// RFC 3261 §24.2, F1 (Alice's INVITE), with its SDP.
    static let rfcInvite = """
    INVITE sip:bob@biloxi.example.com SIP/2.0\r
    Via: SIP/2.0/UDP client.atlanta.example.com:5060;branch=z9hG4bK74bf9\r
    Max-Forwards: 70\r
    From: Alice <sip:alice@atlanta.example.com>;tag=9fxced76sl\r
    To: Bob <sip:bob@biloxi.example.com>\r
    Call-ID: 3848276298220188511@atlanta.example.com\r
    CSeq: 1 INVITE\r
    Contact: <sip:alice@client.atlanta.example.com;transport=tcp>\r
    Content-Type: application/sdp\r
    Content-Length: 151\r
    \r
    v=0\r
    o=alice 2890844526 2890844526 IN IP4 client.atlanta.example.com\r
    s=-\r
    c=IN IP4 192.0.2.101\r
    t=0 0\r
    m=audio 49172 RTP/AVP 0\r
    a=rtpmap:0 PCMU/8000\r

    """

    @Test func parsesRFCExample() throws {
        let message = try SIPMessage(parsing: Data(Self.rfcInvite.utf8))
        #expect(message.method == "INVITE")
        #expect(message.requestURI == "sip:bob@biloxi.example.com")
        #expect(message.callID == "3848276298220188511@atlanta.example.com")
        #expect(message.cseq?.number == 1)
        #expect(message.cseq?.method == "INVITE")
        #expect(message.topViaBranch == "z9hG4bK74bf9")
        #expect(message.from?.displayName == "Alice")
        #expect(message.from?.tag == "9fxced76sl")
        #expect(message.from?.user == "alice")
        #expect(message.to?.tag == nil)
        #expect(message.contact?.uri == "sip:alice@client.atlanta.example.com;transport=tcp")
        #expect(message.body.count == 151)
        let media = try SDP.negotiate(message.body, supported: [.pcmu])
        #expect(media.address == "192.0.2.101")
        #expect(media.port == 49172)
        #expect(media.codec == .pcmu)
    }

    @Test func parsesResponseWithCompactHeadersFoldingAndBareLF() throws {
        let text = "SIP/2.0 180 Ringing\nv: SIP/2.0/UDP 192.0.2.10:5062;branch=z9hG4bKabc;rport=5062\nf: <sip:620@fritz.box>;tag=a\nt: <sip:5550100@fritz.box>\n ;tag=b\ni: xyz\nCSeq: 7 INVITE\nl: 0\n\n"
        let message = try SIPMessage(parsing: Data(text.utf8))
        #expect(message.status == 180)
        #expect(message.topViaBranch == "z9hG4bKabc")
        #expect(message.to?.tag == "b")
        #expect(message.callID == "xyz")
        #expect(message.headers["Content-Length"] == "0")
    }

    @Test func cutsBodyToContentLengthAndRejectsShortBodies() throws {
        let head = "SIP/2.0 200 OK\r\nCSeq: 1 OPTIONS\r\nContent-Length: 4\r\n\r\n"
        let message = try SIPMessage(parsing: Data((head + "abcdEXTRA").utf8))
        #expect(message.body == Data("abcd".utf8))
        #expect(throws: SIPMessage.ParseError.truncatedBody(expected: 4, actual: 2)) {
            try SIPMessage(parsing: Data((head + "ab").utf8))
        }
    }

    @Test(arguments: [
        "",
        "\r\n\r\n",
        "HELLO",
        "INVITE sip:a@b",
        "INVITE sip:a@b HTTP/1.1\r\n\r\n",
        "SIP/2.0 99 Too Low\r\n\r\n",
        "SIP/2.0 700 Too High\r\n\r\n",
        "SIP/2.0 abc OK\r\n\r\n",
        "INV;TE sip:a@b SIP/2.0\r\n\r\n",
        "OPTIONS sip:a@b SIP/2.0\r\nNoColonHere\r\n\r\n",
        "OPTIONS sip:a@b SIP/2.0\r\n : empty name\r\n\r\n",
    ])
    func rejectsGarbage(text: String) {
        #expect(throws: SIPMessage.ParseError.self) { try SIPMessage(parsing: Data(text.utf8)) }
    }

    @Test func survivesRandomBytes() {
        var generator = SystemRandomNumberGenerator()
        let fragments = ["SIP/2.0 ", "INVITE ", "\r\n", ":", ";", "<", ">", "\"", "Via", "Content-Length: 99", " ", "\n\n", "=", "tag"]
        for _ in 0..<2000 {
            let text = (0..<Int.random(in: 1...20, using: &generator)).map { _ in fragments.randomElement(using: &generator)! }.joined()
            let message = try? SIPMessage(parsing: Data(text.utf8))
            // Whatever parses must serialize and parse again.
            if let message {
                _ = message.from
                _ = message.topViaBranch
                #expect((try? SIPMessage(parsing: message.serialized())) != nil)
            }
        }
        for _ in 0..<500 {
            let bytes = (0..<Int.random(in: 0...300, using: &generator)).map { _ in UInt8.random(in: 0...255, using: &generator) }
            _ = try? SIPMessage(parsing: Data(bytes))
            _ = RTPPacket(parsing: Data(bytes))
        }
    }

    @Test func serializationRoundTrips() throws {
        let original = try SIPMessage(parsing: Data(Self.rfcInvite.utf8))
        let data = original.serialized()
        let text = String(decoding: data, as: UTF8.self)
        #expect(text.hasPrefix("INVITE sip:bob@biloxi.example.com SIP/2.0\r\n"))
        #expect(text.contains("Content-Length: 151\r\n\r\nv=0"))
        #expect(try SIPMessage(parsing: data) == original)
    }

    @Test func splitsHeaderListsOutsideQuotesAndBrackets() {
        var headers = SIPHeaders()
        headers.add("Record-Route", "<sip:p1.example.com;lr>, <sip:p2.example.com;lr>")
        headers.add("Record-Route", "<sip:p3.example.com;lr>")
        headers.add("Contact", "\"Doe, John\" <sip:j@example.com>;expires=60")
        #expect(headers.values("record-route") == ["<sip:p1.example.com;lr>", "<sip:p2.example.com;lr>", "<sip:p3.example.com;lr>"])
        #expect(headers.values("m").count == 1)
        #expect(SIPAddress(parsing: headers["Contact"]!)?.displayName == "Doe, John")
        #expect(SIPAddress(parsing: headers["Contact"]!)?.parameter("expires") == "60")
    }

    @Test func parsesAddresses() {
        let bare = SIPAddress(parsing: "sip:620@fritz.box;tag=x1")
        #expect(bare?.uri == "sip:620@fritz.box")
        #expect(bare?.tag == "x1")
        #expect(SIPAddress(parsing: "<sip:anonymous@anonymous.invalid>")?.user == "anonymous")
        #expect(SIPAddress(parsing: "<tel:+495550100;phone-context=x>")?.user == "+495550100")
        #expect(SIPAddress(parsing: "<>") == nil)

        var address = SIPAddress(displayName: "A \"B\"", uri: "sip:1@h")
        address.tag = "t"
        #expect(address.headerValue == "\"A \\\"B\\\"\" <sip:1@h>;tag=t")
        #expect(SIPAddress(parsing: address.headerValue)?.displayName == "A \"B\"")
    }

    @Test func buildsURIsWithEscapedDialCodes() {
        #expect(SIPURI.make(user: "*31#5550100", host: "fritz.box") == "sip:*31%235550100@fritz.box")
        #expect(SIPURI.user(of: "sip:*31%235550100@fritz.box") == "*31#5550100")
        #expect(SIPURI.make(user: nil, host: "fe80::1", port: 5060) == "sip:[fe80::1]:5060")
    }
}

struct SIPDigestTests {
    /// RFC 2617 §3.5 (the HTTP example; the algorithm is the same for SIP).
    @Test func matchesRFC2617Vector() {
        let challenge = SIPDigestChallenge(header: #"Digest realm="testrealm@host.com", qop="auth,auth-int", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", opaque="5ccc069c403ebaf9f0171e9517f40e41""#)
        #expect(challenge?.qop == ["auth", "auth-int"])
        let header = SIPDigest.authorization(
            challenge: challenge!, method: "GET", uri: "/dir/index.html",
            username: "Mufasa", password: "Circle Of Life", cnonce: "0a4f113b", nonceCount: 1
        )
        #expect(header.contains(#"response="6629fae49393a05397450978507c4ef1""#))
        #expect(header.contains("nc=00000001"))
        #expect(header.contains(#"opaque="5ccc069c403ebaf9f0171e9517f40e41""#))
    }

    @Test func withoutQopUsesTheOldFormula() {
        let challenge = SIPDigestChallenge(header: #"Digest realm="fritz.box", nonce="abc""#)!
        let header = SIPDigest.authorization(challenge: challenge, method: "REGISTER", uri: "sip:fritz.box", username: "620", password: "pw")
        let ha1 = SIPDigest.md5Hex("620:fritz.box:pw")
        let ha2 = SIPDigest.md5Hex("REGISTER:sip:fritz.box")
        #expect(header.contains("response=\"\(SIPDigest.md5Hex("\(ha1):abc:\(ha2)"))\""))
        #expect(!header.contains("qop"))
    }

    @Test func parsesChallengeVariants() {
        let challenge = SIPDigestChallenge(header: #"digest realm="a, b",nonce=xyz ,stale=TRUE, algorithm=MD5"#)
        #expect(challenge?.realm == "a, b")
        #expect(challenge?.nonce == "xyz")
        #expect(challenge?.stale == true)
        #expect(challenge?.supportsMD5 == true)
        #expect(SIPDigestChallenge(header: #"Digest realm="r", nonce="n", algorithm=SHA-256"#)?.supportsMD5 == false)
        #expect(SIPDigestChallenge(header: #"Basic realm="r""#) == nil)
        #expect(SIPDigestChallenge(header: #"Digest nonce="n""#) == nil)
    }
}

struct SDPTests {
    @Test func picksPCMAFromAFritzBoxOffer() throws {
        let media = try SDP.negotiate(Data(FakeFritzBox.offer.utf8))
        #expect(media == SDPMedia(address: "192.0.2.1", port: 7078, codec: .pcma, telephoneEventPayloadType: 101, direction: .sendrecv, ptime: 20))
    }

    @Test func refusesOffersWithoutACommonCodec() {
        let offer = "v=0\r\nc=IN IP4 192.0.2.1\r\nm=audio 4000 RTP/AVP 9 18\r\na=rtpmap:9 G722/8000\r\na=rtpmap:18 G729/8000\r\n"
        #expect(throws: SDP.NegotiationError.noCommonCodec) { try SDP.negotiate(Data(offer.utf8)) }
        #expect(throws: SDP.NegotiationError.notSDP) { try SDP.negotiate(Data()) }
        #expect(throws: SDP.NegotiationError.noAudio) { try SDP.negotiate(Data("v=0\r\nc=IN IP4 192.0.2.1\r\nm=video 4000 RTP/AVP 96\r\n".utf8)) }
        #expect(throws: SDP.NegotiationError.missingAddress) { try SDP.negotiate(Data("v=0\r\nm=audio 4000 RTP/AVP 8\r\n".utf8)) }
    }

    @Test func mediaLevelWinsAndStaticTypesNeedNoRtpmap() throws {
        let offer = "v=0\nc=IN IP4 198.51.100.1\na=sendonly\nm=audio 5004 RTP/AVP 8\nc=IN IP4 192.0.2.7/127\n"
        let media = try SDP.negotiate(Data(offer.utf8))
        #expect(media.address == "192.0.2.7")
        #expect(media.direction == .sendonly)
        #expect(media.telephoneEventPayloadType == nil)
    }

    @Test func ourOfferRoundTrips() throws {
        let sdp = SDP.make(address: "192.0.2.10", port: 20000, sessionID: 1)
        let text = String(decoding: sdp, as: UTF8.self)
        #expect(text.contains("m=audio 20000 RTP/AVP 8 101\r\n"))
        #expect(text.contains("a=rtpmap:8 PCMA/8000\r\n"))
        let media = try SDP.negotiate(sdp)
        #expect(media.port == 20000)
        #expect(media.codec == .pcma)
        #expect(media.telephoneEventPayloadType == 101)
    }
}

struct RTPTests {
    @Test func roundTrips() throws {
        let packet = RTPPacket(payloadType: 8, marker: true, sequenceNumber: 0xFFFF, timestamp: 0xDEADBEEF, ssrc: 0x01020304, payload: Data(repeating: 0xD5, count: 160))
        let data = packet.serialized()
        #expect(data.count == 172)
        #expect(Array(data.prefix(12)) == [0x80, 0x88, 0xFF, 0xFF, 0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04])
        #expect(RTPPacket(parsing: data) == packet)
    }

    @Test func skipsCSRCsExtensionAndPadding() throws {
        // V=2, P=1, X=1, CC=1; one CSRC; extension with one word; 2 bytes padding.
        var bytes: [UInt8] = [0xB1, 0x08, 0, 1, 0, 0, 0, 160, 0, 0, 0, 9]
        bytes += [0, 0, 0, 7]
        bytes += [0xBE, 0xDE, 0, 1, 1, 2, 3, 4]
        bytes += [0xAA, 0xBB]
        bytes += [0, 2]
        let packet = try #require(RTPPacket(parsing: Data(bytes)))
        #expect(packet.payload == Data([0xAA, 0xBB]))
        #expect(packet.ssrc == 9)
        #expect(RTPPacket(parsing: Data([0x40] + [UInt8](repeating: 0, count: 11))) == nil)
        #expect(RTPPacket(parsing: Data([0x80, 0x08])) == nil)
    }

    @Test func senderCountsSamples() throws {
        var sender = RTPSender(payloadType: 8, ssrc: 42, sequenceNumber: 65535, timestamp: 1000)
        let firstData = sender.audio(Data(count: 160))
        let secondData = sender.audio(Data(count: 160))
        let first = try #require(RTPPacket(parsing: firstData))
        let second = try #require(RTPPacket(parsing: secondData))
        #expect(first.marker && !second.marker)
        #expect(first.sequenceNumber == 65535 && second.sequenceNumber == 0)
        #expect(second.timestamp == 1160)
    }

    @Test func rawAudioIsCutInto20msFrames() {
        let frames = CallAudio.frames(fromRaw: Data(repeating: 0x2A, count: 240))
        #expect(frames.count == 2)
        #expect(frames.allSatisfy { $0.count == 160 })
        #expect(frames[1].prefix(80) == Data(repeating: 0x2A, count: 80))
        #expect(frames[1].suffix(80) == Data(repeating: G711.aLawSilence, count: 80))
        #expect(CallAudio.frames(fromRaw: Data()).isEmpty)
    }

    @Test func dtmfFollowsRFC4733() throws {
        var sender = RTPSender(payloadType: 8, telephoneEventPayloadType: 101, ssrc: 1, sequenceNumber: 10, timestamp: 0)
        let digit = sender.dtmf("#", milliseconds: 60)
        let packets = try #require(digit).compactMap { RTPPacket(parsing: $0) }
        #expect(packets.count == 5)
        #expect(packets.allSatisfy { $0.payloadType == 101 && $0.timestamp == 0 && $0.payload[0] == 11 })
        #expect(packets[0].marker && !packets[1].marker)
        #expect(packets.map { $0.payload[1] & 0x80 != 0 } == [false, false, true, true, true])
        #expect(packets.last.map { UInt16($0.payload[2]) << 8 | UInt16($0.payload[3]) } == 480)
        #expect(Array(packets.map(\.sequenceNumber)) == [10, 11, 12, 13, 14])
        let nextData = sender.audio(Data(count: 160))
        let next = try #require(RTPPacket(parsing: nextData))
        #expect(next.timestamp == 480 && next.sequenceNumber == 15)
        let unknown = sender.dtmf("x")
        #expect(unknown == nil)
        var plain = RTPSender(payloadType: 8)
        let withoutEvents = plain.dtmf("1")
        #expect(withoutEvents == nil)
    }
}
