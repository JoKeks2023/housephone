import Contacts
import HousephoneKit
import SwiftUI

struct ContactsView: View {
    @Environment(FritzBoxData.self) private var fritzBox
    @AppStorage("contacts.source") private var source: ListSource = .fritzBox

    private var showsFritzBox: Bool { fritzBox.showsPhonebook && source == .fritzBox }

    var body: some View {
        NavigationStack {
            Group {
                if showsFritzBox {
                    FritzBoxContactsList()
                        .transition(.opacity)
                } else {
                    DeviceContactsContent()
                        .transition(.opacity)
                }
            }
            .motion(Theme.Motion.snappy, value: showsFritzBox)
            .navigationTitle("Kontakte")
            .listSourceMenu($source, isAvailable: fritzBox.showsPhonebook)
        }
    }
}

/// The iPhone's own contacts.
private struct DeviceContactsContent: View {
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(CallCenter.self) private var callCenter
    @Environment(\.openURL) private var openURL
    @State private var searchText = ""
    @State private var selected: ContactEntry?

    var body: some View {
        content
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
                    if searchText.isEmpty {
                        FavoritesSection()
                    }
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
                .appBackground()
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

    @Environment(FavoritesStore.self) private var favorites

    var body: some View {
        Button(action: onCall) {
            HStack(spacing: Theme.Space.s3) {
                AvatarView(name: contact.name, imageData: contact.thumbnail, size: 40)
                VStack(alignment: .leading, spacing: Theme.Space.hairline) {
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
                            .accessibilityLabel(Text.spokenNumber(number.value))
                    }
                }
                Spacer()
                if contact.numbers.contains(where: { favorites.contains(number: $0.value) }) {
                    Image(systemName: "star.fill")
                        .font(.footnote)
                        .foregroundStyle(Theme.accentText)
                        .accessibilityLabel(Text("Favorit"))
                }
                Image(systemName: "phone")
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(RowButtonStyle())
        .accessibilityElement(children: .combine)
        .accessibilityHint(Text("Anrufen"))
        .contextMenu {
            if contact.numbers.count == 1, let number = contact.numbers.first {
                Button("Anrufen", systemImage: "phone", action: onCall)
                Button("Nummer kopieren", systemImage: "doc.on.doc") {
                    UIPasteboard.general.string = number.value
                }
            }
            ForEach(contact.numbers, id: \.self) { number in
                if favorites.contains(number: number.value) {
                    Button(favoriteTitle(remove: true, number), systemImage: "star.slash") {
                        favorites.remove(number: number.value)
                    }
                } else {
                    Button(favoriteTitle(remove: false, number), systemImage: "star") {
                        favorites.add(name: contact.name, number: number.value, label: number.label, contactID: contact.id)
                    }
                }
            }
        }
    }

    /// One entry per number; with several numbers the label tells them apart.
    private func favoriteTitle(remove: Bool, _ number: ContactEntry.Number) -> String {
        if contact.numbers.count == 1 {
            return remove ? String(localized: "Aus Favoriten entfernen") : String(localized: "Zu Favoriten")
        }
        let which = number.label ?? number.value
        return remove
            ? String(localized: "\(which) aus Favoriten entfernen")
            : String(localized: "\(which) zu Favoriten")
    }
}
