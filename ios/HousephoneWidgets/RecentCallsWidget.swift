import HousephoneKit
import SwiftUI
import WidgetKit

/// The latest calls; missed ones stand out. Tapping a call calls back.
struct RecentCallsWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "RecentCalls", provider: SnapshotProvider()) { entry in
            RecentCallsWidgetView(entry: entry)
                .containerBackground(.background, for: .widget)
        }
        .configurationDisplayName("Anrufe")
        .description("Deine letzten Anrufe, verpasste hervorgehoben.")
        .supportedFamilies([.systemMedium, .systemLarge])
    }
}

struct RecentCallsWidgetView: View {
    let entry: SnapshotEntry
    @Environment(\.widgetFamily) private var family

    private var rows: Int { family == .systemLarge ? 7 : 3 }

    var body: some View {
        let snapshot = entry.snapshot
        VStack(alignment: .leading, spacing: 6) {
            Link(destination: SnapshotEntry.recentsURL) {
                HStack {
                    Text("Anrufe")
                        .font(.headline)
                    Spacer()
                    let missed = snapshot.newMissedCalls.count
                    if missed > 0 {
                        Label("Verpasst: \(missed)", systemImage: "phone.arrow.down.left")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(Color("DangerText"))
                    }
                }
            }
            if !snapshot.isSetUp {
                WidgetMessage(symbol: "phone.badge.waveform", text: "Richte Housephone in der App ein.")
            } else if snapshot.recentCalls.isEmpty {
                WidgetMessage(symbol: "clock", text: "Noch keine Anrufe.")
            } else {
                ForEach(snapshot.recentCalls.prefix(rows)) { call in
                    row(call)
                }
                Spacer(minLength: 0)
            }
        }
    }

    @ViewBuilder
    private func row(_ call: SharedSnapshot.RecentCall) -> some View {
        let content = HStack(spacing: 8) {
            Image(systemName: call.direction == .outgoing ? "phone.arrow.up.right" : "phone.arrow.down.left")
                .font(.footnote)
                .foregroundStyle(call.isMissed ? Color("DangerText") : .secondary)
                .frame(width: 18)
                .accessibilityHidden(true)
            Text(title(call))
                .font(.subheadline.weight(call.isMissed ? .semibold : .regular))
                .foregroundStyle(call.isMissed ? Color("DangerText") : .primary)
                .lineLimit(1)
            Spacer()
            Text(callTime(call.date))
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        if PhoneNumber.dialable(call.number) != nil {
            Link(destination: entry.callURL(number: call.number, name: call.name)) { content }
                .accessibilityLabel(Text("\(title(call)) zurückrufen"))
        } else {
            content
        }
    }

    private func title(_ call: SharedSnapshot.RecentCall) -> String {
        if let name = call.name, !name.isEmpty { return name }
        return call.number.isEmpty ? String(localized: "Unbekannt") : call.number
    }
}
