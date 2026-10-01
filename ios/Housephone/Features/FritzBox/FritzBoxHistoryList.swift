import HousephoneKit
import SwiftUI

/// The line's call list from the FRITZ!Box: every call, also those taken
/// at other phones or by the answering machine.
struct FritzBoxHistoryList: View {
    let missedOnly: Bool

    @Environment(FritzBoxData.self) private var fritzBox
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(CallCenter.self) private var callCenter

    private var resource: FritzBoxResource<FritzBoxCallList> { fritzBox.history }

    var body: some View {
        Group {
            if let history = resource.value {
                list(history)
            } else if let failure = resource.failure {
                FritzBoxFailureView(failure: failure) {
                    Task { await fritzBox.refreshHistory() }
                }
            } else {
                ProgressView("Anrufliste wird geladen …")
            }
        }
        .task { await fritzBox.refreshHistory() }
    }

    @ViewBuilder
    private func list(_ history: FritzBoxCallList) -> some View {
        let calls = missedOnly ? history.calls.filter(\.isMissed) : history.calls
        Group {
            if calls.isEmpty {
                if missedOnly {
                    EmptyStateView(symbol: "phone.arrow.down.left", title: "Keine verpassten Anrufe", message: "Alles erledigt.")
                } else {
                    EmptyStateView(symbol: "clock", title: "Keine Anrufe", message: "Die Anrufliste der FRITZ!Box ist leer.")
                }
            } else {
                List {
                    if let failure = resource.failure {
                        FritzBoxStaleNotice(failure: failure, fetchedAt: resource.fetchedAt)
                    }
                    ForEach(calls) { call in
                        CallHistoryRow(
                            name: displayName(for: call),
                            number: call.number,
                            symbol: call.symbol,
                            detail: [call.outcomeText, call.durationText].compactMap(\.self).joined(separator: " · "),
                            isMissed: call.isMissed,
                            date: call.startedAt,
                            imageData: contacts.thumbnail(for: call.number)
                        ) {
                            callBack(call)
                        }
                    }
                }
                .listStyle(.plain)
                .appBackground()
                .motion(Theme.Motion.standard, value: missedOnly)
            }
        }
        .refreshable { await fritzBox.refreshHistory() }
    }

    /// iPhone contact first, then the name the FRITZ!Box recorded.
    private func displayName(for call: FritzBoxCall) -> String? {
        if !call.number.isEmpty, let name = contacts.name(for: call.number) { return name }
        guard let name = call.name?.trimmingCharacters(in: .whitespaces), !name.isEmpty else { return nil }
        return name
    }

    private func callBack(_ call: FritzBoxCall) {
        guard !call.number.isEmpty else { return }
        let name = displayName(for: call)
        Task { await callCenter.startCall(to: call.number, name: name) }
    }
}
