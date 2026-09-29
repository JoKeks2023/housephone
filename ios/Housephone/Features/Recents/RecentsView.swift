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
    @Query(sort: \CallRecord.date, order: .reverse) private var records: [CallRecord]
    @State private var filter: Filter = .all

    private var visibleRecords: [CallRecord] {
        filter == .all ? records : records.filter(\.isMissed)
    }

    var body: some View {
        NavigationStack {
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
                            RecentRow(record: record, contactName: contacts.name(for: record.number)) {
                                call(record)
                            }
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
                }
            }
            .navigationTitle("Anrufe")
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Picker("Filter", selection: $filter) {
                        Text("Alle").tag(Filter.all)
                        Text("Verpasst").tag(Filter.missed)
                    }
                    .pickerStyle(.segmented)
                    .frame(width: 200)
                }
                if !records.isEmpty {
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            Button("Alle Einträge löschen", systemImage: "trash", role: .destructive) {
                                deleteAll()
                            }
                        } label: {
                            Label("Mehr", systemImage: "ellipsis")
                        }
                    }
                }
            }
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

private struct RecentRow: View {
    let record: CallRecord
    let contactName: String?
    let onCall: () -> Void

    private var title: String {
        if let contactName { return contactName }
        if let name = record.name, !name.isEmpty { return name }
        return record.number.isEmpty ? String(localized: "Unbekannt") : record.number
    }

    private var directionSymbol: String {
        switch (record.direction, record.outcome) {
        case (.incoming, .missed): "phone.arrow.down.left.fill"
        case (.incoming, _): "phone.arrow.down.left"
        case (.outgoing, _): "phone.arrow.up.right"
        }
    }

    var body: some View {
        Button(action: onCall) {
            HStack(spacing: Theme.Space.s3) {
                AvatarView(name: contactName ?? record.name, size: 40)

                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.body.weight(.medium))
                        .foregroundStyle(record.isMissed ? Theme.danger : Color.primary)
                        .lineLimit(1)
                    HStack(spacing: Theme.Space.s1) {
                        Image(systemName: directionSymbol)
                            .imageScale(.small)
                            .accessibilityHidden(true)
                        Text(subtitle)
                    }
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                }

                Spacer(minLength: Theme.Space.s2)

                Text(record.date, format: Self.dateFormat(for: record.date))
                    .font(.subheadline)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(record.number.isEmpty)
        .accessibilityElement(children: .combine)
        .accessibilityHint(Text("Zurückrufen"))
    }

    private var subtitle: String {
        var parts: [String] = []
        if contactName != nil || record.name != nil, !record.number.isEmpty {
            parts.append(record.number)
        }
        if let duration = record.duration, duration >= 1 {
            parts.append(duration.callDurationText)
        } else {
            parts.append(String(localized: record.outcome.label))
        }
        return parts.joined(separator: " · ")
    }

    private static func dateFormat(for date: Date) -> Date.FormatStyle {
        Calendar.current.isDateInToday(date)
            ? .dateTime.hour().minute()
            : .dateTime.day().month(.abbreviated)
    }
}
