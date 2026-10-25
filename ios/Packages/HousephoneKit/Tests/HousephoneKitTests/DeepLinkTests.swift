import Foundation
import Testing
@testable import HousephoneKit

struct DeepLinkTests {
    @Test(arguments: [
        DeepLink.call(number: "030123456", name: "Test", key: "abc"),
        DeepLink.call(number: "+4930123456", name: nil, key: nil),
        DeepLink.call(number: "**610", name: "Alle Telefone", key: nil),
        DeepLink.keypad,
        DeepLink.recents(missedOnly: false),
        DeepLink.recents(missedOnly: true),
    ])
    func roundTrips(link: DeepLink) {
        #expect(DeepLink(url: link.url) == link)
    }

    @Test func cleansTheNumber() {
        let url = URL(string: "housephone://call?number=030%20123-456")!
        #expect(DeepLink(url: url) == .call(number: "030123456", name: nil, key: nil))
    }

    @Test(arguments: [
        "housephone://call",
        "housephone://call?number=",
        "housephone://call?number=abc",
        "housephone://pair?bridge=wss://example.org&code=123",
        "tel:030123456",
        "https://call?number=030123456",
    ])
    func rejects(link: String) {
        #expect(DeepLink(url: URL(string: link)!) == nil)
    }

    @Test func keyIsCreatedOnceAndCompared() throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        #expect(DeepLinkKey.load(from: directory) == nil)

        let key = try DeepLinkKey.loadOrCreate(in: directory)
        #expect(key.count >= 40)
        #expect(try DeepLinkKey.loadOrCreate(in: directory) == key)
        #expect(DeepLinkKey.load(from: directory) == key)

        #expect(DeepLinkKey.matches(key, expected: key))
        #expect(!DeepLinkKey.matches(nil, expected: key))
        #expect(!DeepLinkKey.matches(key, expected: nil))
        #expect(!DeepLinkKey.matches(String(key.dropLast()), expected: key))
        #expect(!DeepLinkKey.matches("", expected: ""))
    }
}
