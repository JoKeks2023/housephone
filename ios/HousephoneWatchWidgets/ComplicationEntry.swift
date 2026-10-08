import HousephoneKit
import SwiftUI
import WidgetKit

/// Snapshot the watch app wrote into its App Group, plus the link key so a
/// tap on a favorite calls without asking.
struct ComplicationData {
    let snapshot: SharedSnapshot
    let linkKey: String?

    static func load() -> ComplicationData {
        ComplicationData(
            snapshot: SharedSnapshotStore.appGroup?.load() ?? .empty,
            linkKey: AppGroup.containerURL.flatMap(DeepLinkKey.load(from:))
        )
    }

    func callURL(_ favorite: SharedSnapshot.Favorite) -> URL {
        DeepLink.call(number: favorite.number, name: favorite.name, key: linkKey).url
    }

    /// Made-up favorites for previews and the face editor.
    static let preview = ComplicationData(
        snapshot: SharedSnapshot(
            favorites: [SharedSnapshot.Favorite(id: UUID(), name: "Oma", number: "030 123456", label: "Privat")],
            recentCalls: [SharedSnapshot.RecentCall(id: "1", number: "030 123456", name: "Oma", direction: .incoming, outcome: .missed, date: .now.addingTimeInterval(-600))],
            isSetUp: true
        ),
        linkKey: nil
    )
}

/// Missed calls count for 24 hours, like the watch's home screen.
let missedWindow: TimeInterval = 24 * 3600

extension SharedSnapshot {
    func missedCalls(at date: Date) -> [RecentCall] {
        recentCalls.filter { $0.isMissed && $0.date > date.addingTimeInterval(-missedWindow) && $0.date <= date }
    }
}

/// Initials on the person's identity color, for circular complications.
struct ComplicationAvatar: View {
    let name: String

    var body: some View {
        ZStack {
            AccessoryWidgetBackground()
            if let initials = Monogram.initials(for: name) {
                Text(initials)
                    .font(.system(.title3, design: .rounded).weight(.semibold))
                    .minimumScaleFactor(0.6)
            } else {
                Image(systemName: "person.fill")
            }
        }
        .widgetAccentable()
    }
}
