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
                        FritzBoxCallRow(call: call, name: displayName(for: call)) {
                            callBack(call)
                        }
                    }
                }
                .listStyle(.plain)
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

private struct FritzBoxCallRow: View {
    let call: FritzBoxCall
    let name: String?
    let onCall: () -> Void

    private var title: String {
        if let name { return name }
        return call.number.isEmpty ? String(localized: "Unbekannt") : call.number
    }

    private var subtitle: String {
        var parts: [String] = []
        if name != nil, !call.number.isEmpty { parts.append(call.number) }
        parts.append(call.outcomeText)
        if let duration = call.durationText { parts.append(duration) }
        return parts.joined(separator: " · ")
    }

    private var tint: Color {
        call.isMissed ? Theme.danger : .primary
    }

    var body: some View {
        Button(action: onCall) {
            HStack(spacing: Theme.Space.s3) {
                AvatarView(name: name, size: 40)

                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.body.weight(.medium))
                        .foregroundStyle(tint)
                        .lineLimit(1)
                    HStack(spacing: Theme.Space.s1) {
                        Image(systemName: call.symbol)
                            .imageScale(.small)
                            .accessibilityHidden(true)
                        Text(subtitle)
                    }
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                }

                Spacer(minLength: Theme.Space.s2)

                Text(call.startedAt, format: Self.dateFormat(for: call.startedAt))
                    .font(.subheadline)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(call.number.isEmpty)
        .accessibilityElement(children: .combine)
        .accessibilityHint(Text("Zurückrufen"))
    }

    private static func dateFormat(for date: Date) -> Date.FormatStyle {
        Calendar.current.isDateInToday(date)
            ? .dateTime.hour().minute()
            : .dateTime.day().month(.abbreviated)
    }
}
