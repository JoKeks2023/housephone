import Foundation
import HousephoneKit

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

extension FritzBoxCall {
    /// SF Symbol for direction and result.
    var symbol: String {
        switch (direction, result) {
        case (.incoming, .missed): "phone.arrow.down.left.fill"
        case (.incoming, .rejected): "phone.down"
        case (.incoming, .answered) where isAnsweringMachine: "recordingtape"
        case (.incoming, _): "phone.arrow.down.left"
        case (.outgoing, _): "phone.arrow.up.right"
        default: "phone"
        }
    }

    /// What happened, e.g. "am Mobilteil 1" or "Verpasst".
    var outcomeText: String {
        let device = device?.trimmingCharacters(in: .whitespaces) ?? ""
        switch (direction, result) {
        case (_, .active):
            return String(localized: "Läuft gerade")
        case (.incoming, .missed):
            return String(localized: "Verpasst")
        case (.incoming, .rejected):
            return String(localized: "Abgewiesen")
        case (.incoming, .answered) where isAnsweringMachine:
            return String(localized: "Anrufbeantworter")
        case (.incoming, _) where !device.isEmpty:
            return String(localized: "am \(device)")
        case (.outgoing, _) where !device.isEmpty:
            return String(localized: "von \(device)")
        case (.outgoing, _):
            return String(localized: "Ausgehend")
        default:
            return String(localized: "Angenommen")
        }
    }

    /// "5 Min." – the FRITZ!Box counts whole minutes. `nil` when not connected.
    var durationText: String? {
        guard durationSeconds > 0 else { return nil }
        return Duration.seconds(durationSeconds).formatted(.units(allowed: [.hours, .minutes], width: .abbreviated))
    }
}
