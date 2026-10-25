import HousephoneKit
import SwiftUI
import WidgetKit

/// Lock screen (and watch-style accessory) widget: missed calls since the
/// recents were last looked at.
struct MissedCallsWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "MissedCalls", provider: SnapshotProvider()) { entry in
            MissedCallsWidgetView(entry: entry)
                .containerBackground(.clear, for: .widget)
                .widgetURL(SnapshotEntry.missedCallsURL)
        }
        .configurationDisplayName("Verpasste Anrufe")
        .description("Zeigt neue verpasste Anrufe.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

struct MissedCallsWidgetView: View {
    let entry: SnapshotEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        let missed = entry.snapshot.newMissedCalls
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                VStack(spacing: 0) {
                    Image(systemName: "phone.arrow.down.left")
                        .font(.caption)
                    Text("\(missed.count)")
                        .font(.title3.weight(.semibold))
                        .contentTransition(.numericText())
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("Verpasst: \(missed.count)"))
        case .accessoryInline:
            if missed.isEmpty {
                Label("Keine verpassten Anrufe", systemImage: "phone")
            } else {
                Label("Verpasst: \(missed.count)", systemImage: "phone.arrow.down.left")
            }
        default:
            if let latest = missed.first {
                VStack(alignment: .leading, spacing: 0) {
                    Label("Verpasst: \(missed.count)", systemImage: "phone.arrow.down.left")
                        .font(.headline)
                        .widgetAccentable()
                    Text(latest.name ?? (latest.number.isEmpty ? String(localized: "Unbekannt") : latest.number))
                        .lineLimit(1)
                    Text(callTime(latest.date))
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                Label("Keine verpassten Anrufe", systemImage: "phone")
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}
