import HousephoneKit
import SwiftUI

/// The FRITZ!Box phonebook: favorites first, then A–Z.
struct FritzBoxContactsList: View {
    @Environment(FritzBoxData.self) private var fritzBox
    @Environment(CallCenter.self) private var callCenter
    @State private var searchText = ""
    @State private var choosingNumberFor: FritzBoxContact?

    private var resource: FritzBoxResource<FritzBoxPhonebook> { fritzBox.phonebook }

    var body: some View {
        Group {
            if let phonebook = resource.value {
                list(phonebook)
            } else if let failure = resource.failure {
                FritzBoxFailureView(failure: failure) {
                    Task { await fritzBox.refreshPhonebook() }
                }
            } else {
                ProgressView("Telefonbuch wird geladen …")
            }
        }
        .task { await fritzBox.refreshPhonebook() }
        .confirmationDialog(
            choosingNumberFor?.name ?? "",
            isPresented: Binding(get: { choosingNumberFor != nil }, set: { if !$0 { choosingNumberFor = nil } }),
            titleVisibility: .visible,
            presenting: choosingNumberFor
        ) { contact in
            ForEach(contact.numbers, id: \.self) { number in
                Button {
                    call(contact, number)
                } label: {
                    Text("\(String(localized: number.type.label)): \(number.number)")
                }
            }
        }
    }

    // MARK: - List

    @ViewBuilder
    private func list(_ phonebook: FritzBoxPhonebook) -> some View {
        let query = searchText.trimmingCharacters(in: .whitespaces)
        let matches = Self.filter(phonebook.contacts, query: query)
        Group {
            if phonebook.contacts.isEmpty {
                EmptyStateView(
                    symbol: "book.closed",
                    title: "Telefonbuch ist leer",
                    message: "Kontakte mit Nummer aus dem FRITZ!Box-Telefonbuch erscheinen hier."
                )
            } else if matches.isEmpty {
                ContentUnavailableView.search(text: query)
            } else {
                List {
                    if let failure = resource.failure {
                        FritzBoxStaleNotice(failure: failure, fetchedAt: resource.fetchedAt)
                    }
                    if query.isEmpty {
                        FavoritesSection(showsHint: false)
                    }
                    let favorites = matches.filter(\.favorite)
                    if query.isEmpty, !favorites.isEmpty {
                        Section("FRITZ!Box-Favoriten") {
                            ForEach(favorites) { contact in
                                row(contact)
                            }
                        }
                    }
                    ForEach(Self.sections(matches), id: \.letter) { section in
                        Section(section.letter) {
                            ForEach(section.contacts) { contact in
                                row(contact)
                            }
                        }
                    }
                }
                .listStyle(.plain)
                .appBackground()
            }
        }
        .searchable(text: $searchText, prompt: Text("Name oder Nummer"))
        .refreshable { await fritzBox.refreshPhonebook() }
    }

    private func row(_ contact: FritzBoxContact) -> some View {
        FritzBoxContactRow(contact: contact) {
            if contact.numbers.count == 1, let number = contact.numbers.first {
                call(contact, number)
            } else {
                choosingNumberFor = contact
            }
        }
    }

    private func call(_ contact: FritzBoxContact, _ number: FritzBoxNumber) {
        Task { await callCenter.startCall(to: number.number, name: contact.name) }
    }

    // MARK: - Filtering and grouping

    static func filter(_ contacts: [FritzBoxContact], query: String) -> [FritzBoxContact] {
        let sorted = contacts.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        guard !query.isEmpty else { return sorted }
        let digits = query.filter(\.isNumber)
        return sorted.filter { contact in
            contact.name.localizedStandardContains(query)
                || (!digits.isEmpty && contact.numbers.contains { $0.number.filter(\.isNumber).contains(digits) })
        }
    }

    static func sections(_ contacts: [FritzBoxContact]) -> [(letter: String, contacts: [FritzBoxContact])] {
        let grouped = Dictionary(grouping: contacts) { contact -> String in
            let folded = contact.name.folding(options: [.diacriticInsensitive, .caseInsensitive], locale: .current)
            guard let first = folded.first, first.isLetter else { return "#" }
            return String(first).uppercased()
        }
        return grouped.keys.sorted { lhs, rhs in
            if lhs == "#" { return false }
            if rhs == "#" { return true }
            return lhs < rhs
        }.map { ($0, grouped[$0] ?? []) }
    }
}

private struct FritzBoxContactRow: View {
    let contact: FritzBoxContact
    let onCall: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        Button(action: onCall) {
            HStack(spacing: Theme.Space.s3) {
                AvatarView(name: contact.name, size: 36)
                VStack(alignment: .leading, spacing: Theme.Space.hairline) {
                    HStack(spacing: Theme.Space.s1) {
                        Text(contact.name)
                            .font(.body.weight(.medium))
                            .foregroundStyle(.primary)
                            .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
                        if contact.favorite {
                            Image(systemName: "star.fill")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                                .accessibilityLabel(Text("Favorit"))
                        }
                    }
                    subtitle
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
                }
                Spacer()
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
            ForEach(contact.numbers, id: \.self) { number in
                Button("Nummer kopieren: \(String(localized: number.type.label))", systemImage: "doc.on.doc") {
                    UIPasteboard.general.string = number.number
                }
            }
        }
    }

    @ViewBuilder
    private var subtitle: some View {
        if contact.numbers.count > 1 {
            Text("\(contact.numbers.count) Nummern")
        } else if let number = contact.numbers.first {
            Text("\(String(localized: number.type.label)) · \(number.number)")
                .monospacedDigit()
                .accessibilityLabel(Text(AttributedString("\(String(localized: number.type.label)), ") + .spokenNumber(number.number)))
        }
    }
}
