import HousephoneKit
import SwiftData
import SwiftUI

/// Who called and every call with that number, like the Phone app's info
/// screen: the person on top, the main actions, then the history.
struct CallDetailsSheet: View {
    let number: String
    /// Name recorded with the call, used when no contact matches.
    let recordedName: String?

    @Environment(\.dismiss) private var dismiss
    @Environment(CallCenter.self) private var callCenter
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(FavoritesStore.self) private var favorites
    @Environment(AppModel.self) private var appModel
    @Query(sort: \CallRecord.date, order: .reverse) private var records: [CallRecord]

    private var name: String? { contacts.name(for: number) ?? recordedName }

    private var calls: [CallRecord] {
        guard let key = PhoneNumber.matchKey(number) else { return [] }
        return records.filter { PhoneNumber.matchKey($0.number) == key }
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    header
                        .listRowBackground(Color.clear)
                        .listRowInsets(EdgeInsets())
                }

                Section("Anrufe") {
                    ForEach(calls) { record in
                        CallDetailsRow(record: record)
                    }
                }
            }
            .appBackground(.grouped)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Fertig", systemImage: "checkmark") { dismiss() }
                }
            }
        }
    }

    private var header: some View {
        VStack(spacing: Theme.Space.s4) {
            AvatarView(name: name, imageData: contacts.thumbnail(for: number), size: 96)
            VStack(spacing: Theme.Space.s1) {
                Text(name ?? (number.isEmpty ? String(localized: "Unbekannt") : number))
                    .font(.title2.weight(.semibold))
                    .multilineTextAlignment(.center)
                if name != nil, !number.isEmpty {
                    Text(number)
                        .font(.callout)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .accessibilityLabel(Text.spokenNumber(number))
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)

            GlassEffectContainer(spacing: Theme.Space.s3) {
                HStack(spacing: Theme.Space.s3) {
                    DetailAction(symbol: "phone.fill", label: "Anrufen", isProminent: true) {
                        let name = name
                        dismiss()
                        Task { await callCenter.startCall(to: number, name: name) }
                    }
                    DetailAction(symbol: "circle.grid.3x3.fill", label: "Tastenfeld") {
                        appModel.keypadNumber = number
                        appModel.selectedTab = .keypad
                        dismiss()
                    }
                    DetailAction(symbol: "doc.on.doc", label: "Kopieren") {
                        UIPasteboard.general.string = number
                    }
                    DetailAction(
                        symbol: favorites.contains(number: number) ? "star.fill" : "star",
                        label: favorites.contains(number: number) ? "Favorit" : "Favorisieren"
                    ) {
                        toggleFavorite()
                    }
                }
            }
            .disabled(number.isEmpty)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, Theme.Space.s4)
    }

    private func toggleFavorite() {
        if favorites.contains(number: number) {
            favorites.remove(number: number)
        } else {
            let contact = contacts.contact(for: number)
            let label = contact?.numbers.first { PhoneNumber.matchKey($0.value) == PhoneNumber.matchKey(number) }?.label
            favorites.add(name: name ?? number, number: number, label: label, contactID: contact?.id)
        }
    }
}

/// One of the round actions under the person, like in Contacts.
private struct DetailAction: View {
    let symbol: String
    let label: LocalizedStringKey
    var isProminent = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: Theme.Space.s1) {
                Image(systemName: symbol)
                    .font(.title3.weight(.medium))
                    .contentTransition(.symbolEffect(.replace))
                    .frame(width: 52, height: 52)
                    .foregroundStyle(isProminent ? Color.white : Theme.accentText)
                    .background {
                        if isProminent { Circle().fill(Theme.call) }
                    }
                    .glassEffect(isProminent ? Glass.identity : Glass.regular.interactive(), in: .circle)
                Text(label)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            .frame(minWidth: 64)
        }
        .buttonStyle(PressableButtonStyle())
        .accessibilityLabel(Text(label))
    }
}

/// Date and time, direction, outcome and duration of one call.
private struct CallDetailsRow: View {
    let record: CallRecord

    var body: some View {
        HStack(spacing: Theme.Space.s3) {
            Image(systemName: record.direction == .incoming ? "phone.arrow.down.left" : "phone.arrow.up.right")
                .foregroundStyle(record.isMissed ? Theme.dangerText : .secondary)
                .frame(width: 24)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: Theme.Space.hairline) {
                Text(record.date.formatted(date: .abbreviated, time: .shortened))
                    .font(.body)
                Text(directionText)
                    .font(.subheadline)
                    .foregroundStyle(record.isMissed ? Theme.dangerText : .secondary)
            }
            Spacer(minLength: Theme.Space.s2)
            if let duration = record.duration, duration >= 1 {
                Text(duration.callDurationText)
                    .font(.subheadline)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var directionText: String {
        let direction = record.direction == .incoming
            ? String(localized: "Eingehend")
            : String(localized: "Ausgehend")
        return record.outcome == .answered ? direction : "\(direction) · \(String(localized: record.outcome.label))"
    }
}
