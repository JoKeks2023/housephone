import Foundation
import HousephoneKit
import Observation

/// A favorite is one number of a person, like in the Phone app.
struct Favorite: Codable, Hashable, Identifiable, Sendable {
    var id = UUID()
    /// The iPhone contact it came from, if any; the name and number are
    /// kept too, so a favorite survives a deleted contact.
    var contactID: String?
    var name: String
    var number: String
    /// "Mobil", "Privat" …, as shown under the name.
    var label: String?
}

/// Favorites, stored on this iPhone only (they are personal and don't
/// belong to the bridge or the FRITZ!Box).
@MainActor
@Observable
final class FavoritesStore {
    private(set) var favorites: [Favorite] = []

    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let key = "favorites.v1"

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        if let data = defaults.data(forKey: key),
           let stored = try? JSONDecoder().decode([Favorite].self, from: data) {
            favorites = stored
        }
    }

    func contains(number: String) -> Bool {
        favorite(for: number) != nil
    }

    func favorite(for number: String) -> Favorite? {
        guard let key = PhoneNumber.matchKey(number) else { return nil }
        return favorites.first { PhoneNumber.matchKey($0.number) == key }
    }

    func add(name: String, number: String, label: String? = nil, contactID: String? = nil) {
        guard !number.isEmpty, !contains(number: number) else { return }
        favorites.append(Favorite(contactID: contactID, name: name, number: number, label: label))
        save()
    }

    func remove(number: String) {
        guard let key = PhoneNumber.matchKey(number) else { return }
        favorites.removeAll { PhoneNumber.matchKey($0.number) == key }
        save()
    }

    func remove(_ favorite: Favorite) {
        favorites.removeAll { $0.id == favorite.id }
        save()
    }

    private func save() {
        if let data = try? JSONEncoder().encode(favorites) {
            defaults.set(data, forKey: key)
        }
    }
}
