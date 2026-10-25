import AppIntents
import HousephoneKit
import Observation
import os
import SwiftData
import UIKit
import WidgetKit

/// Keeps what the system shows outside the app current: the App Group
/// snapshot for widgets and controls, the home screen quick actions and
/// the favorites Siri knows for App Shortcuts.
@MainActor
final class SnapshotPublisher {
    private let favorites: FavoritesStore
    private let fritzBox: FritzBoxData
    private let contacts: ContactsDirectory
    private let bridge: BridgeConnection
    private let direct: DirectPhone
    private let appModel: AppModel
    private let modelContainer: ModelContainer
    private let store = SharedSnapshotStore.appGroup
    private var pending: Task<Void, Never>?
    /// Every publish, changed or not (e.g. to hand favorites to the watch).
    var onPublish: ((SharedSnapshot) -> Void)?
    private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "integration")

    init(favorites: FavoritesStore, fritzBox: FritzBoxData, contacts: ContactsDirectory, bridge: BridgeConnection, direct: DirectPhone, appModel: AppModel, modelContainer: ModelContainer) {
        self.favorites = favorites
        self.fritzBox = fritzBox
        self.contacts = contacts
        self.bridge = bridge
        self.direct = direct
        self.appModel = appModel
        self.modelContainer = modelContainer
        observe()
        setNeedsPublish()
    }

    /// Publishes shortly after the last of several quick changes.
    func setNeedsPublish() {
        pending?.cancel()
        pending = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            self?.publish()
        }
    }

    func publish() {
        pending?.cancel()
        pending = nil
        let snapshot = makeSnapshot()
        updateQuickActions(snapshot.favorites)
        onPublish?(snapshot)
        guard let store else { return }
        do {
            guard try store.save(snapshot) else { return }
            WidgetCenter.shared.reloadAllTimelines()
            if #available(iOS 18, *) {
                // A renamed favorite shows up in its control.
                ControlCenter.shared.reloadAllControls()
            }
            HousephoneShortcuts.updateAppShortcutParameters()
        } catch {
            logger.error("Writing the snapshot failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Observation reports one change per registration, so this registers
    /// again after each one. New calls in the recents come through
    /// `CallCenter.onCallRecorded`.
    private func observe() {
        withObservationTracking {
            _ = favorites.favorites
            _ = fritzBox.history.value
            _ = appModel.recentsSeenAt
            _ = bridge.isPaired
            _ = direct.isEnabled
        } onChange: { [self] in
            // Lives as long as the app; a strong reference is fine.
            Task { @MainActor in
                self.setNeedsPublish()
                self.observe()
            }
        }
    }

    private func makeSnapshot() -> SharedSnapshot {
        SharedSnapshot(
            favorites: favorites.favorites.map {
                SharedSnapshot.Favorite(id: $0.id, name: $0.name, number: $0.number, label: $0.label)
            },
            recentCalls: recentCalls(),
            recentsSeenAt: appModel.recentsSeenAt,
            isSetUp: bridge.isPaired || direct.isEnabled
        )
    }

    /// From the list the recents tab shows: this iPhone's calls or the
    /// FRITZ!Box call list.
    private func recentCalls() -> [SharedSnapshot.RecentCall] {
        let source = UserDefaults.standard.string(forKey: "recents.source").flatMap(ListSource.init(rawValue:)) ?? .iPhone
        if source == .fritzBox, fritzBox.showsHistory, let calls = fritzBox.history.value?.calls {
            return calls.prefix(SharedSnapshot.recentCallsLimit).map { call in
                SharedSnapshot.RecentCall(
                    id: call.id,
                    number: call.number,
                    name: contacts.name(for: call.number) ?? call.name,
                    direction: call.direction == .outgoing ? .outgoing : .incoming,
                    outcome: call.isMissed ? .missed : (call.result == .rejected ? .declined : .answered),
                    date: call.startedAt
                )
            }
        }
        var descriptor = FetchDescriptor<CallRecord>(sortBy: [SortDescriptor(\.date, order: .reverse)])
        descriptor.fetchLimit = SharedSnapshot.recentCallsLimit
        let records = (try? modelContainer.mainContext.fetch(descriptor)) ?? []
        return records.map { record in
            SharedSnapshot.RecentCall(
                id: record.callId.uuidString,
                number: record.number,
                name: contacts.name(for: record.number) ?? record.name,
                direction: record.direction,
                outcome: record.outcome,
                date: record.date
            )
        }
    }

    /// Up to four favorites when long-pressing the app icon.
    private func updateQuickActions(_ favorites: [SharedSnapshot.Favorite]) {
        UIApplication.shared.shortcutItems = favorites.prefix(4).map { favorite in
            QuickActions.item(for: favorite)
        }
    }
}
