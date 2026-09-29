import HousephoneKit
import SwiftUI

/// The FRITZ!Box phonebook on the wrist: favorites first, then everyone.
struct WatchContactsView: View {
    @Environment(WatchFritzBox.self) private var fritzBox
    @State private var searchText = ""

    private var resource: FritzBoxResource<FritzBoxPhonebook> { fritzBox.phonebook }

    var body: some View {
        Group {
            if let phonebook = resource.value {
                list(phonebook)
            } else if let failure = resource.failure {
                WatchFritzBoxFailure(failure: failure) {
                    Task { await fritzBox.refreshPhonebook() }
                }
            } else {
                ProgressView()
            }
        }
        .navigationTitle("Kontakte")
        .task { await fritzBox.refreshPhonebook() }
    }

    @ViewBuilder
    private func list(_ phonebook: FritzBoxPhonebook) -> some View {
        let query = searchText.trimmingCharacters(in: .whitespaces)
        let matches = Self.filter(phonebook.contacts, query: query)
        List {
            if let failure = resource.failure {
                WatchFritzBoxStaleNotice(failure: failure)
            }
            if phonebook.contacts.isEmpty {
                Text("Das FRITZ!Box-Telefonbuch ist leer.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else if matches.isEmpty {
                Text("Keine Treffer")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                let favorites = matches.filter(\.favorite)
                if query.isEmpty, !favorites.isEmpty {
                    Section("Favoriten") {
                        ForEach(favorites) { contact in
                            WatchContactEntry(contact: contact)
                        }
                    }
                }
                Section(query.isEmpty ? "Alle" : "Treffer") {
                    ForEach(matches) { contact in
                        WatchContactEntry(contact: contact)
                    }
                }
            }
        }
        .searchable(text: $searchText, prompt: Text("Name oder Nummer"))
    }

    static func filter(_ contacts: [FritzBoxContact], query: String) -> [FritzBoxContact] {
        let sorted = contacts.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        guard !query.isEmpty else { return sorted }
        let digits = query.filter(\.isNumber)
        return sorted.filter { contact in
            contact.name.localizedStandardContains(query)
                || (!digits.isEmpty && contact.numbers.contains { $0.number.filter(\.isNumber).contains(digits) })
        }
    }
}

/// One contact: a tap calls, or opens the number choice when there are several.
private struct WatchContactEntry: View {
    let contact: FritzBoxContact
    @Environment(WatchCallCenter.self) private var callCenter

    var body: some View {
        if contact.numbers.count == 1, let number = contact.numbers.first {
            Button {
                Task { await callCenter.startCall(to: number.number, name: contact.name) }
            } label: {
                label(detail: String(localized: number.type.label))
            }
            .accessibilityHint(Text("Anrufen"))
        } else {
            NavigationLink {
                WatchNumberChoice(contact: contact)
            } label: {
                label(detail: String(localized: "\(contact.numbers.count) Nummern"))
            }
        }
    }

    private func label(detail: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: WatchTheme.Space.s1) {
                Text(contact.name)
                    .lineLimit(1)
                if contact.favorite {
                    Image(systemName: "star.fill")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(Text("Favorit"))
                }
            }
            Text(detail)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct WatchNumberChoice: View {
    let contact: FritzBoxContact
    @Environment(WatchCallCenter.self) private var callCenter

    var body: some View {
        List(contact.numbers, id: \.self) { number in
            Button {
                Task { await callCenter.startCall(to: number.number, name: contact.name) }
            } label: {
                VStack(alignment: .leading, spacing: 2) {
                    Text(number.type.label)
                    Text(number.number)
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
            .accessibilityHint(Text("Anrufen"))
        }
        .navigationTitle(contact.name)
    }
}

/// Short note when the shown FRITZ!Box data is not current.
struct WatchFritzBoxStaleNotice: View {
    let failure: FritzBoxLoadFailure

    var body: some View {
        Label {
            Text(failure.watchTitle)
                .font(.footnote)
        } icon: {
            Image(systemName: failure.watchSymbol)
                .foregroundStyle(WatchTheme.warning)
        }
        .listRowBackground(Color.clear)
    }
}

/// Shown instead of a list when there is no FRITZ!Box data at all.
struct WatchFritzBoxFailure: View {
    let failure: FritzBoxLoadFailure
    let retry: () -> Void

    var body: some View {
        ScrollView {
            VStack(spacing: WatchTheme.Space.s2) {
                Image(systemName: failure.watchSymbol)
                    .font(.title2)
                    .foregroundStyle(WatchTheme.warning)
                Text(failure.watchTitle)
                    .font(.headline)
                    .multilineTextAlignment(.center)
                Text(failure.watchMessage)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                if failure != .unauthorized {
                    Button("Erneut versuchen", action: retry)
                        .padding(.top, WatchTheme.Space.s2)
                }
            }
            .frame(maxWidth: .infinity)
        }
    }
}

extension FritzBoxLoadFailure {
    var watchTitle: LocalizedStringResource {
        switch self {
        case .unavailable: "FRITZ!Box nicht verbunden"
        case .unreachable: "Bridge nicht erreichbar"
        case .unauthorized: "Kopplung ungültig"
        }
    }

    var watchMessage: LocalizedStringResource {
        switch self {
        case .unavailable: "In der Bridge ist kein Zugang zur FRITZ!Box eingerichtet."
        case .unreachable: "Telefonbuch und Anrufliste kommen über deine Bridge. Prüfe die Verbindung."
        case .unauthorized: "Koppel die Watch auf dem iPhone neu."
        }
    }

    var watchSymbol: String {
        switch self {
        case .unavailable: "phone.connection"
        case .unreachable: "wifi.slash"
        case .unauthorized: "link.badge.plus"
        }
    }
}

extension FritzBoxNumberType {
    var label: LocalizedStringResource {
        switch self {
        case .home: "Privat"
        case .mobile: "Mobil"
        case .work: "Geschäftlich"
        case .faxWork: "Fax"
        case .intern: "Intern"
        case .memo: "Notiz"
        default: "Sonstige"
        }
    }
}
