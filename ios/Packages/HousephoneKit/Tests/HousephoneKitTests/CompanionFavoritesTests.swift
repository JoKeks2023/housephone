import Foundation
import Testing
@testable import HousephoneKit

struct CompanionFavoritesTests {
    @Test func roundTripsThroughADictionary() {
        let favorites = CompanionFavorites(favorites: [
            .init(id: UUID(), name: "Test", number: "030 123456", label: "Privat"),
            .init(id: UUID(), name: "Praxis", number: "030 654321"),
        ])
        #expect(CompanionFavorites(dictionary: favorites.dictionary) == favorites)
    }

    @Test func sharesTheContextWithThePairingState() {
        // The iPhone's application context carries favorites only; the
        // watch's carries its pairing state. Neither reads the other's key.
        let favorites = CompanionFavorites(favorites: [])
        #expect(WatchPairingState(dictionary: favorites.dictionary) == nil)
        #expect(CompanionFavorites(dictionary: WatchPairingState(phase: .paired).dictionary) == nil)
        #expect(CompanionFavorites(dictionary: [:]) == nil)
    }
}
