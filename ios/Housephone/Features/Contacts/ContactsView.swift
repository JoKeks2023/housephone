import Contacts
import SwiftUI

struct ContactsView: View {
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(CallCenter.self) private var callCenter
    @Environment(\.openURL) private var openURL
    @State private var searchText = ""
    @State private var selected: ContactEntry?

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Kontakte")
        }
    }

    @ViewBuilder
    private var content: some View {
        switch contacts.authorization {
        case .notDetermined:
            EmptyStateView(
                symbol: "person.crop.circle",
                title: "Deine Kontakte",
                message: "Erlaube den Zugriff, um Kontakte direkt anzurufen und Anrufer am Namen zu erkennen."
            ) {
                Button("Zugriff erlauben") {
                    Task { await contacts.requestAccess() }
                }
                .buttonStyle(.glassProminent)
            }
        case .denied, .restricted:
            EmptyStateView(
                symbol: "person.crop.circle.badge.xmark",
                title: "Kein Zugriff auf Kontakte",
                message: "Du kannst den Zugriff in den Einstellungen erlauben. Wählen geht auch ohne – über das Tastenfeld."
            ) {
                Button("Einstellungen öffnen") {
                    if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                }
            }
        default:
            list
        }
    }

    private var filtered: [ContactEntry] {
        let query = searchText.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return contacts.contacts }
        let digits = query.filter(\.isNumber)
        return contacts.contacts.filter { contact in
            contact.name.localizedStandardContains(query)
                || (!digits.isEmpty && contact.numbers.contains { $0.value.filter(\.isNumber).contains(digits) })
        }
    }

    private var sections: [(letter: String, contacts: [ContactEntry])] {
        let grouped = Dictionary(grouping: filtered) { contact -> String in
            guard let first = contact.sortKey.first, first.isLetter else { return "#" }
            return String(first).uppercased()
        }
        return grouped.keys.sorted { lhs, rhs in
            if lhs == "#" { return false }
            if rhs == "#" { return true }
            return lhs < rhs
        }.map { ($0, grouped[$0] ?? []) }
    }

    @ViewBuilder
    private var list: some View {
        Group {
            if contacts.contacts.isEmpty, !contacts.isLoading {
                EmptyStateView(symbol: "person.crop.circle", title: "Keine Kontakte mit Nummer", message: "Kontakte mit Telefonnummer erscheinen hier.")
            } else if filtered.isEmpty, !searchText.isEmpty {
                ContentUnavailableView.search(text: searchText)
            } else {
                List {
                    ForEach(sections, id: \.letter) { section in
                        Section(section.letter) {
                            ForEach(section.contacts) { contact in
                                ContactRow(contact: contact) {
                                    if contact.numbers.count == 1 {
                                        call(contact, number: contact.numbers[0].value)
                                    } else {
                                        selected = contact
                                    }
                                }
                            }
                        }
                    }
                }
                .listStyle(.plain)
            }
        }
        .searchable(text: $searchText, prompt: Text("Name oder Nummer"))
        .confirmationDialog(
            selected?.name ?? "",
            isPresented: Binding(get: { selected != nil }, set: { if !$0 { selected = nil } }),
            titleVisibility: .visible,
            presenting: selected
        ) { contact in
            ForEach(contact.numbers, id: \.self) { number in
                Button {
                    call(contact, number: number.value)
                } label: {
                    Text(number.label.map { "\($0): \(number.value)" } ?? number.value)
                }
            }
        }
    }

    private func call(_ contact: ContactEntry, number: String) {
        Task { await callCenter.startCall(to: number, name: contact.name) }
    }
}

private struct ContactRow: View {
    let contact: ContactEntry
    let onCall: () -> Void

    var body: some View {
        Button(action: onCall) {
            HStack(spacing: Theme.Space.s3) {
                AvatarView(name: contact.name, size: 36)
                VStack(alignment: .leading, spacing: 2) {
                    Text(contact.name)
                        .font(.body.weight(.medium))
                        .foregroundStyle(.primary)
                    if contact.numbers.count > 1 {
                        Text("\(contact.numbers.count) Nummern")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    } else if let number = contact.numbers.first {
                        Text(number.value)
                            .font(.subheadline)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
                Image(systemName: "phone")
                    .foregroundStyle(Color.accentColor)
                    .accessibilityHidden(true)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityHint(Text("Anrufen"))
    }
}
