import Foundation
import Testing
@testable import HousephoneKit

struct G711Tests {
    // Reference values from ITU-T G.711 / Sun's g711.c.
    @Test(arguments: [
        (Int16(0), UInt8(0xD5)),
        (Int16(-1), UInt8(0x55)),
        (Int16(8), UInt8(0xD5)),
        (Int16(16), UInt8(0xD4)),
        (Int16(32767), UInt8(0xAA)),
        (Int16(-32768), UInt8(0x2A)),
        (Int16(1000), UInt8(0xFA)),
        (Int16(-1000), UInt8(0x7A)),
    ])
    func encodesReferenceValues(sample: Int16, code: UInt8) {
        #expect(G711.aLaw(fromLinear: sample) == code)
    }

    @Test(arguments: [
        (UInt8(0xD5), Int16(8)),
        (UInt8(0x55), Int16(-8)),
        (UInt8(0xAA), Int16(32256)),
        (UInt8(0x2A), Int16(-32256)),
        (UInt8(0xFA), Int16(1008)),
    ])
    func decodesReferenceValues(code: UInt8, sample: Int16) {
        #expect(G711.linear(fromALaw: code) == sample)
    }

    @Test func everyCodeSurvivesDecodeEncode() {
        for code in UInt8.min...UInt8.max {
            #expect(G711.aLaw(fromLinear: G711.linear(fromALaw: code)) == code, "code \(code)")
        }
    }

    @Test func quantizationErrorStaysWithinSegmentStep() {
        // A-law keeps ~13 bits of magnitude; the error is at most half a step
        // of the segment (up to 1024 for the loudest segment).
        for sample in stride(from: Int(Int16.min), through: Int(Int16.max), by: 97) {
            let decoded = Int(G711.linear(fromALaw: G711.aLaw(fromLinear: Int16(sample))))
            let magnitude = abs(sample)
            let limit = magnitude < 512 ? 16 : max(16, magnitude / 16)
            #expect(abs(decoded - sample) <= limit, "sample \(sample) → \(decoded)")
        }
    }

    @Test func silenceIsSilent() {
        #expect(G711.decodeALaw(AudioFrame.silence).allSatisfy { abs($0) <= 8 })
    }
}

struct AudioFrameTests {
    @Test func wrapsAndUnwraps() throws {
        let payload = Data((0..<160).map { UInt8(truncatingIfNeeded: $0) })
        let message = try #require(AudioFrame.encode(aLaw: payload))
        #expect(message.count == 161)
        #expect(message.first == 0x01)
        #expect(AudioFrame.decode(message) == payload)
    }

    @Test func rejectsWrongSizes() {
        #expect(AudioFrame.encode(aLaw: Data(count: 159)) == nil)
        #expect(AudioFrame.decode(Data([0x01]) + Data(count: 159)) == nil)
    }

    @Test func ignoresReservedTypes() {
        #expect(AudioFrame.decode(Data([0x02]) + Data(count: 160)) == nil)
    }

    @Test func chunkerEmitsWholeFramesOnly() {
        var chunker = FrameChunker()
        #expect(chunker.append([Int16](repeating: 1, count: 100)).isEmpty)
        let frames = chunker.append([Int16](repeating: 2, count: 250))
        #expect(frames.count == 2)
        #expect(frames.allSatisfy { $0.count == 160 })
        #expect(frames[0].prefix(100).allSatisfy { $0 == 1 })
        #expect(frames[0].suffix(60).allSatisfy { $0 == 2 })
        #expect(chunker.bufferedSampleCount == 30)
    }
}

struct JitterBufferTests {
    func frame(_ n: Int) -> Data { Data([UInt8(truncatingIfNeeded: n)]) }

    @Test func waitsForTargetDepthBeforePlaying() {
        var buffer = JitterBuffer()
        buffer.push(frame(1))
        buffer.push(frame(2))
        #expect(buffer.pull() == .silence)
        buffer.push(frame(3))
        #expect(buffer.pull() == .frame(frame(1)))
        #expect(buffer.pull() == .frame(frame(2)))
    }

    @Test func steadyArrivalsNeverUnderrun() {
        var buffer = JitterBuffer()
        var played = 0
        // One frame per 20-ms tick, pull on every tick after pre-roll.
        for tick in 0..<1000 {
            buffer.push(frame(tick))
            if case .frame = buffer.pull() { played += 1 }
        }
        #expect(buffer.statistics.underruns == 0)
        #expect(played >= 997)
    }

    @Test func underrunConcealsOnceThenSilenceAndGrowsTarget() {
        var buffer = JitterBuffer()
        for n in 1...3 { buffer.push(frame(n)) }
        #expect(buffer.pull() == .frame(frame(1)))
        #expect(buffer.pull() == .frame(frame(2)))
        #expect(buffer.pull() == .frame(frame(3)))
        #expect(buffer.pull() == .concealment(frame(3)))
        #expect(buffer.pull() == .silence)
        #expect(buffer.statistics.underruns == 1)
        #expect(buffer.targetDepth == 4)

        // Restarts only after the larger target is filled.
        for n in 4...6 { buffer.push(frame(n)) }
        #expect(buffer.pull() == .silence)
        buffer.push(frame(7))
        #expect(buffer.pull() == .frame(frame(4)))
    }

    @Test func periodicTCPStallsDoNotCauseRepeatedUnderruns() {
        var buffer = JitterBuffer()
        var pending: [Data] = []
        var underrunsAfterWarmUp = 0
        for tick in 0..<5000 {
            pending.append(frame(tick))
            // A 160-ms TCP stall every 4 s; the delayed frames then arrive at once.
            let stalled = tick >= 200 && tick % 200 < 8
            if !stalled {
                for delayed in pending { buffer.push(delayed) }
                pending.removeAll()
            }
            _ = buffer.pull()
            if tick == 1999 { underrunsAfterWarmUp = buffer.statistics.underruns }
        }
        #expect(buffer.statistics.underruns <= 2)
        #expect(buffer.statistics.underruns == underrunsAfterWarmUp)
        #expect(buffer.statistics.droppedFrames == 0)
    }

    @Test func stallDoesNotBuildUpDelay() {
        var buffer = JitterBuffer()
        for n in 0..<3 { buffer.push(frame(n)) }
        _ = buffer.pull()
        // A 2-s stall delivers 100 frames at once.
        for n in 3..<103 { buffer.push(frame(n)) }
        #expect(buffer.bufferedFrames <= buffer.targetDepth + JitterBuffer.Configuration().overflowSlack)
        #expect(buffer.statistics.droppedFrames > 0)
        // The newest audio survives.
        var last: Data?
        while buffer.bufferedFrames > 0 {
            if case .frame(let data) = buffer.pull() { last = data }
        }
        #expect(last == frame(102))
    }

    @Test func surplusShrinksTargetOverTime() {
        var configuration = JitterBuffer.Configuration()
        configuration.shrinkAfterPulls = 50
        var buffer = JitterBuffer(configuration: configuration)
        // Force a larger target with two underruns.
        buffer.push(frame(0)); buffer.push(frame(1)); buffer.push(frame(2))
        for _ in 0..<4 { _ = buffer.pull() }
        for n in 0..<4 { buffer.push(frame(n)) }
        for _ in 0..<5 { _ = buffer.pull() }
        let raised = buffer.targetDepth
        #expect(raised == 5)

        // Now two frames arrive per tick for a while, then one per tick.
        var n = 0
        for _ in 0..<400 {
            buffer.push(frame(n)); n += 1
            if n % 40 == 0 { buffer.push(frame(n)); n += 1 }
            _ = buffer.pull()
        }
        #expect(buffer.targetDepth < raised)
    }

    @Test func resetForgetsEverything() {
        var buffer = JitterBuffer()
        for n in 0..<5 { buffer.push(frame(n)) }
        buffer.reset()
        #expect(buffer.bufferedFrames == 0)
        #expect(buffer.pull() == .silence)
    }
}

struct CallSessionWebSocketMediaTests {
    let callId = CallID()
    let start = Date(timeIntervalSince1970: 1_800_000_000)

    var media: CallMedia { CallMedia(callId: callId) }

    @Test func incomingAcceptsWithoutAnswer() {
        var (call, _) = CallSession.incoming(push: IncomingCallPush(callId: callId, caller: "+4930123456", callerName: nil, bridgeId: "b"), now: start)
        #expect(call.handle(.webSocketMedia(media)) == [.startWebSocketMedia(media)])
        #expect(call.mediaMode == .webSocket)
        #expect(call.phase == .ringing)
        #expect(call.handle(.userAnswered) == [.sendAccept])
        #expect(call.handle(.remoteState(.connected), now: start + 1) == [])
        #expect(call.phase == .connected)
    }

    @Test func answerBeforeMediaAcceptsWhenMediaArrives() {
        var (call, _) = CallSession.incoming(push: IncomingCallPush(callId: callId, caller: "", callerName: nil, bridgeId: "b"), now: start)
        #expect(call.handle(.userAnswered) == [])
        #expect(call.handle(.webSocketMedia(media)) == [.startWebSocketMedia(media), .sendAccept])
        // A re-attach repeats call.media; accept goes out only once.
        #expect(call.handle(.webSocketMedia(media)) == [.startWebSocketMedia(media)])
    }

    @Test func outgoingStartsMediaAndRingback() {
        var call = CallSession.outgoing(id: callId, number: "+4930123456", name: nil, now: start)
        #expect(call.handle(.webSocketMedia(media)) == [.startWebSocketMedia(media)])
        #expect(call.handle(.remoteState(.ringing)) == [.startRingback])
        #expect(call.handle(.remoteState(.connected), now: start + 3) == [.stopRingback, .reportOutgoingConnected])
    }

    @Test func formatSupport() {
        #expect(media.isSupported)
        #expect(!CallMedia(callId: callId, codec: "G722", sampleRate: 16000).isSupported)
    }
}

struct CompanionLinkTests {
    @Test func pairingInstructionRoundTrips() throws {
        let pairing = CompanionPairing(code: "M4QX7ZP2KD", url: URL(string: "wss://phone.example.com/v1/ws")!, expiresAt: Date(timeIntervalSince1970: 1_800_000_600))
        let instruction = CompanionPairingInstruction(pairing: pairing, bridgeName: "Zuhause")
        let decoded = try #require(CompanionPairingInstruction(dictionary: instruction.dictionary))
        #expect(decoded == instruction)
        #expect(decoded.link.bridgeName == "Zuhause")
        #expect(decoded.isExpired(at: Date(timeIntervalSince1970: 1_800_000_599)) == false)
        #expect(decoded.isExpired(at: Date(timeIntervalSince1970: 1_800_000_600)))
    }

    @Test func instructionIgnoresOtherMessages() {
        #expect(CompanionPairingInstruction(dictionary: ["type": "something"]) == nil)
        #expect(CompanionPairingInstruction(dictionary: [
            "type": CompanionPairingInstruction.messageType, "url": "wss://x/v1/ws", "code": "invalid", "expiresAt": 0.0,
        ]) == nil)
    }

    @Test func watchStateRoundTripsThroughPropertyList() throws {
        let state = WatchPairingState(phase: .paired, bridgeName: "Zuhause", canReceiveCalls: true, updatedAt: Date(timeIntervalSince1970: 1_800_000_000))
        let dictionary = state.dictionary
        // WatchConnectivity requires property-list values.
        #expect(PropertyListSerialization.propertyList(dictionary, isValidFor: .binary))
        #expect(WatchPairingState(dictionary: dictionary) == state)
        #expect(WatchPairingState(dictionary: [:]) == nil)
    }

    @Test func unpairInstructionRoundTrips() throws {
        let instruction = CompanionUnpairInstruction(issuedAt: Date(timeIntervalSince1970: 1_800_000_000))
        let dictionary = instruction.dictionary
        #expect(PropertyListSerialization.propertyList(dictionary, isValidFor: .binary))
        #expect(CompanionUnpairInstruction(dictionary: dictionary) == instruction)
        #expect(companionMessageType(of: dictionary) == CompanionUnpairInstruction.messageType)
        // Pairing and unpairing messages never mix up.
        #expect(CompanionUnpairInstruction(dictionary: ["type": CompanionPairingInstruction.messageType, "issuedAt": 0.0]) == nil)
        #expect(CompanionPairingInstruction(dictionary: dictionary) == nil)
    }

    @Test func lateUnpairInstructionKeepsANewerPairing() {
        let issued = Date(timeIntervalSince1970: 1_800_000_000)
        let instruction = CompanionUnpairInstruction(issuedAt: issued)
        #expect(instruction.applies(toPairingAt: nil))
        #expect(instruction.applies(toPairingAt: issued.addingTimeInterval(-60)))
        #expect(instruction.applies(toPairingAt: issued))
        #expect(!instruction.applies(toPairingAt: issued.addingTimeInterval(60)))
    }
}
