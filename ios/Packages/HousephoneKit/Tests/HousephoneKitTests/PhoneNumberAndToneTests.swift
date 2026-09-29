import Foundation
import Testing
@testable import HousephoneKit

struct PhoneNumberTests {
    @Test(arguments: [
        ("+49 (30) 123-456", "+4930123456"),
        ("030 / 12 34 56", "030123456"),
        ("**610", "**610"),
        ("#31#030123", "#31#030123"),
        ("0171\u{00A0}1234567", "01711234567"),
    ])
    func stripsFormatting(input: String, expected: String) {
        #expect(PhoneNumber.dialable(input) == expected)
    }

    @Test(arguments: ["", "+", "abc", "030+123", "030 123 ext. 4"])
    func rejectsNonNumbers(input: String) {
        #expect(PhoneNumber.dialable(input) == nil)
    }

    @Test func matchKeyUnifiesNotations() {
        let key = PhoneNumber.matchKey("030 123456")
        #expect(key == "030123456")
        #expect(PhoneNumber.matchKey("+49 30 123456") == key)
        #expect(PhoneNumber.matchKey("0049 30 123456") == key)
        #expect(PhoneNumber.matchKey("+44 20 7946 0000") == "00442079460000")
        #expect(PhoneNumber.matchKey("**610") == nil)
    }

    @Test func dtmfValidation() {
        #expect(PhoneNumber.isDTMF("12#*"))
        #expect(!PhoneNumber.isDTMF(""))
        #expect(!PhoneNumber.isDTMF("+1"))
    }
}

struct RingbackToneTests {
    @Test func germanToneIsFiveSecondWAV() throws {
        let data = RingbackTone.germanWAV(sampleRate: 8_000)
        #expect(data.count == 44 + 5 * 8_000 * 2)
        #expect(String(decoding: data[0..<4], as: UTF8.self) == "RIFF")
        #expect(String(decoding: data[8..<12], as: UTF8.self) == "WAVE")
        #expect(String(decoding: data[36..<40], as: UTF8.self) == "data")

        let sampleRate = data[24..<28].withUnsafeBytes { $0.loadUnaligned(as: UInt32.self) }
        #expect(UInt32(littleEndian: sampleRate) == 8_000)
    }

    @Test func silenceAfterTone() {
        let data = RingbackTone.germanWAV(sampleRate: 8_000)
        let silence = data[(44 + 8_000 * 2)...]
        #expect(silence.allSatisfy { $0 == 0 })
        let tone = data[44..<(44 + 8_000 * 2)]
        #expect(tone.contains { $0 != 0 })
    }
}
