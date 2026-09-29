import HousephoneKit
import SwiftData
import SwiftUI

struct RecentsView: View {
    enum Filter: Hashable {
        case all
        case missed
    }

    @Environment(\.modelContext) private var modelContext
    @Environment(CallCenter.self) private var callCenter
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(FritzBoxData.self) private var fritzBox
    @Query(sort: \CallRecord.date, order: .reverse) private var records: [CallRecord]
    @State private var filter: Filter = .all
    @State private var confirmsDeleteAll = false
    @AppStorage("recents.source") private var source: ListSource = .iPhone

    private var showsFritzBox: Bool { fritzBox.showsHistory && source == .fritzBox }

    private var visibleRecords: [CallRecord] {
        filter == .all ? records : records.filter(\.isMissed)
    }

    var body: some View {
        NavigationStack {
            Group {
                if showsFritzBox {
                    FritzBoxHistoryList(missedOnly: filter == .missed)
                        .transition(.opacity)
                } else {
                    housephoneList
                        .transition(.opacity)
                }
            }
            .motion(Theme.Motion.snappy, value: showsFritzBox)
            .navigationTitle("Anrufe")
            .listSourceMenu($source, isAvailable: fritzBox.showsHistory)
            .safeAreaBar(edge: .top) {
                Picker("Filter", selection: $filter) {
                    Text("Alle").tag(Filter.all)
                    Text("Verpasst").tag(Filter.missed)
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, Theme.Space.s4)
                .padding(.bottom, Theme.Space.s2)
            }
            .toolbar {
                if !showsFritzBox, !records.isEmpty {
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            Button("Alle Einträge löschen", systemImage: "trash", role: .destructive) {
                                confirmsDeleteAll = true
                            }
                        } label: {
                            Label("Mehr", systemImage: "ellipsis")
                        }
                    }
                }
            }
            .confirmationDialog("Alle Anrufe löschen?", isPresented: $confirmsDeleteAll, titleVisibility: .visible) {
                Button("Alle löschen", role: .destructive) { deleteAll() }
            } message: {
                Text("Die Anrufliste in Housephone wird geleert. Die Anrufliste der Telefon-App bleibt, wie sie ist.")
            }
        }
    }

    /// Calls made or received with Housephone on this iPhone.
    private var housephoneList: some View {
        Group {
            if records.isEmpty {
                EmptyStateView(
                    symbol: "clock",
                    title: "Noch keine Anrufe",
                    message: "Anrufe über Housephone erscheinen hier – und in der Telefon-App."
                )
            } else if visibleRecords.isEmpty {
                EmptyStateView(symbol: "phone.arrow.down.left", title: "Keine verpassten Anrufe", message: "Alles erledigt.")
            } else {
                List {
                    ForEach(visibleRecords) { record in
                        recentRow(record)
                            .swipeActions(edge: .trailing) {
                                Button(role: .destructive) {
                                    delete(record)
                                } label: {
                                    Label("Löschen", systemImage: "trash")
                                }
                            }
                    }
                }
                .listStyle(.plain)
                .motion(Theme.Motion.standard, value: filter)
            }
        }
    }

    private func recentRow(_ record: CallRecord) -> some View {
        let contactName = contacts.name(for: record.number)
        let recordedName = record.name.flatMap { $0.isEmpty ? nil : $0 }
        let detail: String = if let duration = record.duration, duration >= 1 {
            duration.callDurationText
        } else {
            String(localized: record.outcome.label)
        }
        return CallHistoryRow(
            name: contactName ?? recordedName,
            number: record.number,
            symbol: Self.symbol(for: record),
            detail: detail,
            isMissed: record.isMissed,
            date: record.date
        ) {
            call(record)
        }
    }

    private static func symbol(for record: CallRecord) -> String {
        switch (record.direction, record.outcome) {
        case (.incoming, .missed): "phone.arrow.down.left.fill"
        case (.incoming, _): "phone.arrow.down.left"
        case (.outgoing, _): "phone.arrow.up.right"
        }
    }

    private func call(_ record: CallRecord) {
        guard !record.number.isEmpty else { return }
        let name = contacts.name(for: record.number) ?? record.name
        Task { await callCenter.startCall(to: record.number, name: name) }
    }

    private func delete(_ record: CallRecord) {
        modelContext.delete(record)
        try? modelContext.save()
    }

    private func deleteAll() {
        try? modelContext.delete(model: CallRecord.self)
        try? modelContext.save()
    }
}

/// Where a list's entries come from. Calls and Contacts offer the same
/// choice with the same words, in the same order.
enum ListSource: String, CaseIterable {
    case iPhone
    case fritzBox

    var title: LocalizedStringKey {
        switch self {
        case .iPhone: "Dieses iPhone"
        case .fritzBox: "FRITZ!Box"
        }
    }

    var symbol: String {
        switch self {
        case .iPhone: "iphone"
        case .fritzBox: "house"
        }
    }
}

extension View {
    /// The source switch as a title menu with the current source as the
    /// subtitle, like Mail's mailboxes. Only when the FRITZ!Box offers the
    /// data; otherwise the plain large title stays.
    func listSourceMenu(_ source: Binding<ListSource>, isAvailable: Bool) -> some View {
        modifier(ListSourceMenu(source: source, isAvailable: isAvailable))
    }
}

private struct ListSourceMenu: ViewModifier {
    @Binding var source: ListSource
    let isAvailable: Bool

    func body(content: Content) -> some View {
        if isAvailable {
            content
                .navigationBarTitleDisplayMode(.inline)
                .navigationSubtitle(Text(source.title))
                .toolbarTitleMenu {
                    Picker("Quelle", selection: $source) {
                        ForEach(ListSource.allCases, id: \.self) { option in
                            Label(option.title, systemImage: option.symbol).tag(option)
                        }
                    }
                }
        } else {
            content
        }
    }
}
