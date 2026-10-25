import AppIntents
import HousephoneKit

/// A favorite as Shortcuts and Siri see it. Read from the App Group
/// snapshot, so widgets can use the same query later.
struct FavoriteEntity: AppEntity {
    static var typeDisplayRepresentation: TypeDisplayRepresentation { "Favorit" }
    static var defaultQuery: FavoriteQuery { FavoriteQuery() }

    let id: UUID
    let name: String
    let number: String
    let label: String?

    init(_ favorite: SharedSnapshot.Favorite) {
        id = favorite.id
        name = favorite.name
        number = favorite.number
        label = favorite.label
    }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)", subtitle: "\(label ?? number)")
    }
}

struct FavoriteQuery: EntityStringQuery {
    private var favorites: [FavoriteEntity] {
        (SharedSnapshotStore.appGroup?.load()?.favorites ?? []).map(FavoriteEntity.init)
    }

    func entities(for identifiers: [UUID]) async throws -> [FavoriteEntity] {
        favorites.filter { identifiers.contains($0.id) }
    }

    func entities(matching string: String) async throws -> [FavoriteEntity] {
        NameMatcher.matches(string, in: favorites, name: \.name)
    }

    func suggestedEntities() async throws -> [FavoriteEntity] {
        favorites
    }
}
