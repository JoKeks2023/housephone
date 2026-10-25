import AppIntents
import HousephoneKit
import SwiftUI
import WidgetKit

/// Which favorite the complication calls. On the watch, the face editor
/// offers one recommendation per favorite.
struct SelectFavoriteIntent: WidgetConfigurationIntent {
    static var title: LocalizedStringResource { "Favorit wählen" }
    static var description: IntentDescription { "Welchen Favoriten die Komplikation anruft." }

    @Parameter(title: "Favorit")
    var favorite: FavoriteEntity?

    init() {}

    init(favorite: FavoriteEntity?) {
        self.favorite = favorite
    }
}

struct FavoriteEntry: TimelineEntry {
    let date: Date
    let favorite: SharedSnapshot.Favorite?
    let data: ComplicationData
}

struct FavoriteProvider: AppIntentTimelineProvider {
    func placeholder(in context: Context) -> FavoriteEntry {
        FavoriteEntry(date: .now, favorite: ComplicationData.preview.snapshot.favorites.first, data: .preview)
    }

    func snapshot(for configuration: SelectFavoriteIntent, in context: Context) async -> FavoriteEntry {
        entry(for: configuration, data: context.isPreview ? .preview : .load())
    }

    /// The app reloads the complications when the favorites change.
    func timeline(for configuration: SelectFavoriteIntent, in context: Context) async -> Timeline<FavoriteEntry> {
        Timeline(entries: [entry(for: configuration, data: .load())], policy: .never)
    }

    func recommendations() -> [AppIntentRecommendation<SelectFavoriteIntent>] {
        let favorites = ComplicationData.load().snapshot.favorites.prefix(8)
        guard !favorites.isEmpty else {
            return [AppIntentRecommendation(intent: SelectFavoriteIntent(), description: Text("Favorit"))]
        }
        return favorites.map { favorite in
            AppIntentRecommendation(intent: SelectFavoriteIntent(favorite: FavoriteEntity(favorite)), description: Text(favorite.name))
        }
    }

    /// The configured favorite with today's name and number; without one
    /// (or once deleted on the iPhone) the first favorite.
    private func entry(for configuration: SelectFavoriteIntent, data: ComplicationData) -> FavoriteEntry {
        let favorites = data.snapshot.favorites
        let chosen = configuration.favorite.flatMap { entity in favorites.first { $0.id == entity.id } }
        return FavoriteEntry(date: .now, favorite: chosen ?? favorites.first, data: data)
    }
}

struct FavoriteComplication: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "Favorite", intent: SelectFavoriteIntent.self, provider: FavoriteProvider()) { entry in
            FavoriteComplicationView(entry: entry)
                .containerBackground(.clear, for: .widget)
                .widgetURL(entry.favorite.map(entry.data.callURL))
        }
        .configurationDisplayName("Favorit anrufen")
        .description("Ruft einen Favoriten mit einem Tippen an.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline, .accessoryCorner])
    }
}

struct FavoriteComplicationView: View {
    let entry: FavoriteEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        if let favorite = entry.favorite {
            switch family {
            case .accessoryCircular:
                ComplicationAvatar(name: favorite.name)
                    .accessibilityLabel(Text("\(favorite.name) anrufen"))
            case .accessoryCorner:
                Image(systemName: "phone.fill")
                    .font(.title3)
                    .widgetAccentable()
                    .widgetLabel {
                        Text(favorite.name)
                    }
            case .accessoryInline:
                Label(favorite.name, systemImage: "phone.fill")
            default:
                HStack {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(favorite.name)
                            .font(.headline)
                            .lineLimit(1)
                        Text(favorite.label ?? favorite.number)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 4)
                    Image(systemName: "phone.fill")
                        .font(.title3)
                        .widgetAccentable()
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(Text("\(favorite.name) anrufen"))
            }
        } else {
            switch family {
            case .accessoryInline:
                Label("Keine Favoriten", systemImage: "star")
            case .accessoryRectangular:
                Text("Favoriten legst du auf dem iPhone an.")
                    .font(.footnote)
            default:
                ZStack {
                    AccessoryWidgetBackground()
                    Image(systemName: "star")
                }
                .accessibilityLabel(Text("Keine Favoriten"))
            }
        }
    }
}
