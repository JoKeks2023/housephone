import Foundation
import HousephoneKit
import Observation

/// The iPhone's favorites, as the iPhone last sent them. The watch can't
/// edit them; they are managed on the iPhone.
@MainActor
@Observable
final class WatchFavorites {
    private(set) var favorites: [SharedSnapshot.Favorite]

    @ObservationIgnored private let defaults: UserDefaults
    private static let key = "iPhoneFavorites"

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        favorites = defaults.data(forKey: Self.key)
            .flatMap { try? JSONDecoder().decode([SharedSnapshot.Favorite].self, from: $0) } ?? []
    }

    func update(_ received: CompanionFavorites) {
        guard received.favorites != favorites else { return }
        favorites = received.favorites
        if let data = try? JSONEncoder().encode(favorites) {
            defaults.set(data, forKey: Self.key)
        }
    }
}
