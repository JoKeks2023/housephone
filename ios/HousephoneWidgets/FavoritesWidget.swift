import AppIntents
import HousephoneKit
import SwiftUI
import WidgetKit

/// Which favorites the widget shows; without a choice, the first ones.
struct SelectFavoritesIntent: WidgetConfigurationIntent {
    static var title: LocalizedStringResource { "Favoriten wählen" }
    static var description: IntentDescription { "Welche Favoriten das Widget zeigt." }

    @Parameter(title: "Favoriten")
    var favorites: [FavoriteEntity]?
}

struct FavoritesEntry: TimelineEntry {
    let date: Date
    let base: SnapshotEntry
    let favorites: [SharedSnapshot.Favorite]
}

struct FavoritesProvider: AppIntentTimelineProvider {
    func placeholder(in context: Context) -> FavoritesEntry {
        entry(SnapshotEntry.preview, configuration: SelectFavoritesIntent())
    }

    func snapshot(for configuration: SelectFavoritesIntent, in context: Context) async -> FavoritesEntry {
        let current = SnapshotEntry.current()
        return entry(context.isPreview && current.snapshot.favorites.isEmpty ? .preview : current, configuration: configuration)
    }

    /// The app reloads the widgets whenever a favorite changes.
    func timeline(for configuration: SelectFavoritesIntent, in context: Context) async -> Timeline<FavoritesEntry> {
        Timeline(entries: [entry(.current(), configuration: configuration)], policy: .never)
    }

    /// The chosen favorites in the chosen order, with today's names and
    /// numbers; deleted ones drop out.
    private func entry(_ base: SnapshotEntry, configuration: SelectFavoritesIntent) -> FavoritesEntry {
        let all = base.snapshot.favorites
        let chosen = (configuration.favorites ?? []).compactMap { entity in all.first { $0.id == entity.id } }
        return FavoritesEntry(date: base.date, base: base, favorites: chosen.isEmpty ? all : chosen)
    }
}

struct FavoritesWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "Favorites", intent: SelectFavoritesIntent.self, provider: FavoritesProvider()) { entry in
            FavoritesWidgetView(entry: entry)
                .containerBackground(.background, for: .widget)
        }
        .configurationDisplayName("Favoriten")
        .description("Ruf deine Favoriten mit einem Tippen an.")
        .supportedFamilies([.systemSmall, .systemMedium])
    }
}

struct FavoritesWidgetView: View {
    let entry: FavoritesEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        if !entry.base.snapshot.isSetUp {
            WidgetMessage(symbol: "phone.badge.waveform", text: "Richte Housephone in der App ein.")
        } else if entry.favorites.isEmpty {
            WidgetMessage(symbol: "star", text: "Füge in Housephone Favoriten hinzu.")
        } else if family == .systemSmall, let favorite = entry.favorites.first {
            single(favorite)
                .widgetURL(entry.base.callURL(number: favorite.number, name: favorite.name))
        } else {
            HStack(alignment: .top, spacing: 8) {
                ForEach(entry.favorites.prefix(4)) { favorite in
                    Link(destination: entry.base.callURL(number: favorite.number, name: favorite.name)) {
                        VStack(spacing: 6) {
                            WidgetAvatar(name: favorite.name, size: 52)
                            Text(favorite.name)
                                .font(.caption.weight(.medium))
                                .lineLimit(1)
                            Text(favorite.label ?? favorite.number)
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                        }
                        .frame(maxWidth: .infinity)
                    }
                    .accessibilityLabel(Text("\(favorite.name) anrufen"))
                }
            }
            .frame(maxHeight: .infinity)
        }
    }

    private func single(_ favorite: SharedSnapshot.Favorite) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .top) {
                WidgetAvatar(name: favorite.name, size: 48)
                Spacer()
                Image(systemName: "phone.fill")
                    .font(.headline)
                    .foregroundStyle(.white)
                    .frame(width: 32, height: 32)
                    .background(Color("Call"), in: .circle)
                    .widgetAccentable()
            }
            Spacer(minLength: 0)
            Text(favorite.name)
                .font(.headline)
                .lineLimit(2)
            Text(favorite.label ?? favorite.number)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text("\(favorite.name) anrufen"))
    }
}
