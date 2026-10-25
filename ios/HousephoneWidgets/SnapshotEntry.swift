import HousephoneKit
import SwiftUI
import WidgetKit

/// What every widget shows: the snapshot the app wrote into the App Group.
/// Widgets never call; their links open the app, which does.
struct SnapshotEntry: TimelineEntry {
    let date: Date
    let snapshot: SharedSnapshot
    /// Lets call links start without asking (`DeepLinkKey`).
    let linkKey: String?

    static func current() -> SnapshotEntry {
        SnapshotEntry(
            date: .now,
            snapshot: SharedSnapshotStore.appGroup?.load() ?? .empty,
            linkKey: AppGroup.containerURL.flatMap(DeepLinkKey.load(from:))
        )
    }

    func callURL(number: String, name: String?) -> URL {
        DeepLink.call(number: number, name: name, key: linkKey).url
    }

    static let missedCallsURL = DeepLink.recents(missedOnly: true).url
    static let recentsURL = DeepLink.recents(missedOnly: false).url

    /// For the widget gallery and placeholders: made-up people only.
    static let preview: SnapshotEntry = {
        let now = Date.now
        let favorites = [
            SharedSnapshot.Favorite(id: UUID(), name: "Oma", number: "030 123456", label: "Privat"),
            SharedSnapshot.Favorite(id: UUID(), name: "Alex Beispiel", number: "0151 000000", label: "Mobil"),
            SharedSnapshot.Favorite(id: UUID(), name: "Praxis", number: "030 654321", label: nil),
            SharedSnapshot.Favorite(id: UUID(), name: "Sam Muster", number: "0170 000000", label: "Mobil"),
        ]
        let calls = [
            SharedSnapshot.RecentCall(id: "1", number: "030 123456", name: "Oma", direction: .incoming, outcome: .missed, date: now.addingTimeInterval(-12 * 60)),
            SharedSnapshot.RecentCall(id: "2", number: "0151 000000", name: "Alex Beispiel", direction: .outgoing, outcome: .answered, date: now.addingTimeInterval(-3 * 3600)),
            SharedSnapshot.RecentCall(id: "3", number: "030 654321", name: "Praxis", direction: .incoming, outcome: .answered, date: now.addingTimeInterval(-26 * 3600)),
            SharedSnapshot.RecentCall(id: "4", number: "0170 000000", name: "Sam Muster", direction: .outgoing, outcome: .answered, date: now.addingTimeInterval(-50 * 3600)),
        ]
        return SnapshotEntry(
            date: now,
            snapshot: SharedSnapshot(favorites: favorites, recentCalls: calls, recentsSeenAt: now.addingTimeInterval(-3600), isSetUp: true),
            linkKey: nil
        )
    }()
}

struct SnapshotProvider: TimelineProvider {
    func placeholder(in context: Context) -> SnapshotEntry {
        .preview
    }

    func getSnapshot(in context: Context, completion: @escaping (SnapshotEntry) -> Void) {
        let entry = SnapshotEntry.current()
        completion(context.isPreview && entry.snapshot.recentCalls.isEmpty ? .preview : entry)
    }

    /// The app reloads the widgets whenever the snapshot changes. Midnight
    /// turns "today" into a date.
    func getTimeline(in context: Context, completion: @escaping (Timeline<SnapshotEntry>) -> Void) {
        let tomorrow = Calendar.current.startOfDay(for: .now.addingTimeInterval(24 * 3600))
        completion(Timeline(entries: [.current()], policy: .after(tomorrow)))
    }
}

/// When a call was, like in the Phone app: the time today, else the date.
func callTime(_ date: Date) -> String {
    Calendar.current.isDateInToday(date)
        ? date.formatted(date: .omitted, time: .shortened)
        : date.formatted(.dateTime.day().month(.abbreviated))
}

/// A person's initials on their identity color (as in the app).
struct WidgetAvatar: View {
    let name: String
    var size: CGFloat = 44

    var body: some View {
        ZStack {
            if let initials = Monogram.initials(for: name), let tint = AvatarTint.forName(name) {
                Circle().fill(tint.color.gradient)
                Text(initials)
                    .font(.system(size: size * 0.38, weight: .semibold, design: .rounded))
                    .foregroundStyle(.white)
            } else {
                Circle().fill(.fill.tertiary)
                Image(systemName: "person.fill")
                    .font(.system(size: size * 0.4, weight: .medium))
                    .foregroundStyle(.secondary)
            }
        }
        .frame(width: size, height: size)
        .widgetAccentable()
        .accessibilityHidden(true)
    }
}

/// Short message for empty widgets.
struct WidgetMessage: View {
    let symbol: String
    let text: LocalizedStringKey

    var body: some View {
        VStack(spacing: 6) {
            Image(systemName: symbol)
                .font(.title2)
                .foregroundStyle(Color("AccentText"))
            Text(text)
                .font(.footnote)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
