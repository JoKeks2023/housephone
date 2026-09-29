import Foundation
import HousephoneKit
import SwiftData

/// One entry in the app's own recents list.
@Model
final class CallRecord {
    @Attribute(.unique) var callId: UUID
    var directionRaw: String
    var number: String
    var name: String?
    var date: Date
    var duration: TimeInterval?
    var outcomeRaw: String

    init(callId: UUID, direction: CallDirection, number: String, name: String?, date: Date, duration: TimeInterval?, outcome: CallOutcome) {
        self.callId = callId
        self.directionRaw = direction.rawValue
        self.number = number
        self.name = name
        self.date = date
        self.duration = duration
        self.outcomeRaw = outcome.rawValue
    }

    convenience init?(session: CallSession) {
        guard let outcome = session.outcome else { return nil }
        self.init(
            callId: session.id.uuid,
            direction: session.direction,
            number: session.remoteNumber,
            name: session.remoteName,
            date: session.createdAt,
            duration: session.connectedAt != nil ? session.duration : nil,
            outcome: outcome
        )
    }

    var direction: CallDirection { CallDirection(rawValue: directionRaw) ?? .outgoing }
    var outcome: CallOutcome { CallOutcome(rawValue: outcomeRaw) ?? .failed }

    /// Missed calls are highlighted in the list.
    var isMissed: Bool { direction == .incoming && outcome == .missed }
}

extension CallOutcome {
    var label: LocalizedStringResource {
        switch self {
        case .answered: "Angenommen"
        case .missed: "Verpasst"
        case .declined: "Abgelehnt"
        case .cancelled: "Abgebrochen"
        case .busy: "Besetzt"
        case .failed: "Fehlgeschlagen"
        case .answeredElsewhere: "Anderswo angenommen"
        }
    }
}
