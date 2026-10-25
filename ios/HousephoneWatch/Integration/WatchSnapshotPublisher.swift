import HousephoneKit
import Observation
import os
import WidgetKit

/// Writes the watch's snapshot into its App Group for the complications:
/// the iPhone's favorites and the latest calls (the FRITZ!Box call list
/// when the bridge offers it, else this watch's own calls).
@MainActor
final class WatchSnapshotPublisher {
    private let favorites: WatchFavorites
    private let recents: RecentCalls
    private let fritzBox: WatchFritzBox
    private let bridge: WatchBridge
    private let store = SharedSnapshotStore.appGroup
    private var pending: Task<Void, Never>?
    private let logger = Logger(subsystem: "com.jorisconrad.housephone.watch", category: "integration")

    init(favorites: WatchFavorites, recents: RecentCalls, fritzBox: WatchFritzBox, bridge: WatchBridge) {
        self.favorites = favorites
        self.recents = recents
        self.fritzBox = fritzBox
        self.bridge = bridge
        observe()
        setNeedsPublish()
    }

    func setNeedsPublish() {
        pending?.cancel()
        pending = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            self?.publish()
        }
    }

    private func publish() {
        guard let store else { return }
        do {
            guard try store.save(makeSnapshot()) else { return }
            WidgetCenter.shared.reloadAllTimelines()
            WatchShortcuts.updateAppShortcutParameters()
        } catch {
            logger.error("Writing the snapshot failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Observation reports one change per registration, so this registers
    /// again after each one.
    private func observe() {
        withObservationTracking {
            _ = favorites.favorites
            _ = recents.calls
            _ = fritzBox.history.value
            _ = bridge.isPaired
        } onChange: { [self] in
            // Lives as long as the app; a strong reference is fine.
            Task { @MainActor in
                self.setNeedsPublish()
                self.observe()
            }
        }
    }

    private func makeSnapshot() -> SharedSnapshot {
        let calls: [SharedSnapshot.RecentCall]
        if let history = fritzBox.history.value?.calls {
            calls = history.prefix(SharedSnapshot.recentCallsLimit).map { call in
                SharedSnapshot.RecentCall(
                    id: call.id,
                    number: call.number,
                    name: fritzBox.name(for: call.number) ?? call.name,
                    direction: call.direction == .outgoing ? .outgoing : .incoming,
                    outcome: call.isMissed ? .missed : (call.result == .rejected ? .declined : .answered),
                    date: call.startedAt
                )
            }
        } else {
            calls = recents.calls.map { call in
                SharedSnapshot.RecentCall(id: call.id.uuidString, number: call.number, name: call.name, direction: call.direction, outcome: call.outcome, date: call.date)
            }
        }
        // The watch shows missed calls of the last 24 hours (like its home
        // screen); there is no "seen" on the watch.
        return SharedSnapshot(favorites: favorites.favorites, recentCalls: calls, recentsSeenAt: .distantPast, isSetUp: bridge.isPaired)
    }
}
