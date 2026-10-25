import HousephoneKit
import SwiftUI
import WidgetKit

struct MissedCallsEntry: TimelineEntry {
    let date: Date
    let missed: [SharedSnapshot.RecentCall]
}

struct MissedCallsProvider: TimelineProvider {
    func placeholder(in context: Context) -> MissedCallsEntry {
        MissedCallsEntry(date: .now, missed: ComplicationData.preview.snapshot.missedCalls(at: .now))
    }

    func getSnapshot(in context: Context, completion: @escaping (MissedCallsEntry) -> Void) {
        completion(context.isPreview ? placeholder(in: context) : MissedCallsEntry(date: .now, missed: ComplicationData.load().snapshot.missedCalls(at: .now)))
    }

    /// One entry now and one whenever a missed call drops out of the
    /// 24-hour window. The app reloads on new calls.
    func getTimeline(in context: Context, completion: @escaping (Timeline<MissedCallsEntry>) -> Void) {
        let snapshot = ComplicationData.load().snapshot
        let now = Date.now
        let expiries = snapshot.missedCalls(at: now).map { $0.date.addingTimeInterval(missedWindow) }.sorted()
        let dates = [now] + expiries
        let entries = dates.map { MissedCallsEntry(date: $0, missed: snapshot.missedCalls(at: $0)) }
        completion(Timeline(entries: entries, policy: .never))
    }
}

struct MissedCallsComplication: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "MissedCalls", provider: MissedCallsProvider()) { entry in
            MissedCallsComplicationView(entry: entry)
                .containerBackground(.clear, for: .widget)
                .widgetURL(DeepLink.recents(missedOnly: true).url)
        }
        .configurationDisplayName("Verpasste Anrufe")
        .description("Verpasste Anrufe der letzten 24 Stunden.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline, .accessoryCorner])
    }
}

struct MissedCallsComplicationView: View {
    let entry: MissedCallsEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        let count = entry.missed.count
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                VStack(spacing: 0) {
                    Image(systemName: "phone.arrow.down.left")
                        .font(.caption)
                    Text("\(count)")
                        .font(.title3.weight(.semibold))
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("Verpasst: \(count)"))
        case .accessoryCorner:
            Image(systemName: "phone.arrow.down.left")
                .font(.title3)
                .widgetLabel {
                    Text("Verpasst: \(count)")
                }
        case .accessoryInline:
            Label("Verpasst: \(count)", systemImage: "phone.arrow.down.left")
        default:
            VStack(alignment: .leading, spacing: 0) {
                Label("Verpasst: \(count)", systemImage: "phone.arrow.down.left")
                    .font(.headline)
                    .widgetAccentable()
                if let latest = entry.missed.first {
                    Text(latest.name ?? (latest.number.isEmpty ? String(localized: "Unbekannt") : latest.number))
                        .lineLimit(1)
                    Text(latest.date, style: .time)
                        .foregroundStyle(.secondary)
                } else {
                    Text("Keine in den letzten 24 Stunden")
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
