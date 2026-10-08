import HousephoneKit
@preconcurrency import Intents
import SwiftData

/// Someone Siri can call: one number of a person.
struct CallTarget: Sendable, Equatable {
    let name: String
    let number: String
    let label: String?

    var person: INPerson {
        INPerson(
            personHandle: INPersonHandle(value: number, type: .phoneNumber, label: label.map { INPersonHandleLabel(rawValue: $0) }),
            nameComponents: nil,
            displayName: name,
            image: nil,
            contactIdentifier: nil,
            customIdentifier: nil
        )
    }
}

/// "Hey Siri, ruf Oma mit Housephone an" — also in CarPlay. Handled in the
/// app (no Intents extension): it needs the favorites, contacts and the
/// FRITZ!Box phonebook, and the call starts in the app anyway.
///
/// Siri resolves people from the iPhone's contacts itself and passes their
/// number. Names only the app knows (favorites, FRITZ!Box phonebook) come
/// as a bare name and are looked up here.
final class StartCallIntentHandler: NSObject, INStartCallIntentHandling {
    func resolveContacts(for intent: INStartCallIntent) async -> [INStartCallContactResolutionResult] {
        if intent.destinationType == .redial {
            guard let last = await MainActor.run(body: { AppServices.shared.lastDialedTarget() }) else {
                return [.unsupported(forReason: .noCallHistoryForRedial)]
            }
            return [.success(with: last.person)]
        }
        guard let people = intent.contacts, !people.isEmpty else {
            return [.needsValue()]
        }
        guard people.count == 1, let person = people.first else {
            return [.unsupported(forReason: .multipleContactsUnsupported)]
        }
        if let number = person.personHandle?.value, PhoneNumber.dialable(number) != nil {
            return [.success(with: person)]
        }
        let name = person.displayName
        let targets = await MainActor.run { AppServices.shared.callTargets(matching: name) }
        switch targets.count {
        case 0: return [.unsupported(forReason: .noContactFound)]
        case 1: return [.success(with: targets[0].person)]
        default: return [.disambiguation(with: targets.map(\.person))]
        }
    }

    func resolveCallCapability(for intent: INStartCallIntent) async -> INStartCallCallCapabilityResolutionResult {
        switch intent.callCapability {
        case .videoCall: .unsupported(forReason: .videoCallUnsupported)
        default: .success(with: .audioCall)
        }
    }

    func resolveDestinationType(for intent: INStartCallIntent) async -> INCallDestinationTypeResolutionResult {
        switch intent.destinationType {
        case .redial: .success(with: .redial)
        case .normal, .unknown: .success(with: .normal)
        // Emergency calls belong to the Phone app; there's no voicemail.
        default: .unsupported()
        }
    }

    /// The app opens and starts the call (`onContinueUserActivity`).
    func handle(intent: INStartCallIntent) async -> INStartCallIntentResponse {
        INStartCallIntentResponse(code: .continueInApp, userActivity: nil)
    }
}

extension AppServices {
    /// People matching a spoken name: favorites first, then the iPhone's
    /// contacts, then the FRITZ!Box phonebook; each number once.
    func callTargets(matching name: String) -> [CallTarget] {
        var targets: [CallTarget] = []
        var seen: Set<String> = []
        func add(_ target: CallTarget) {
            guard let key = PhoneNumber.matchKey(target.number) ?? PhoneNumber.dialable(target.number),
                  seen.insert(key).inserted
            else { return }
            targets.append(target)
        }

        for favorite in NameMatcher.matches(name, in: favorites.favorites, name: \.name) {
            add(CallTarget(name: favorite.name, number: favorite.number, label: favorite.label))
        }
        for contact in NameMatcher.matches(name, in: contacts.contacts, name: \.name) {
            for number in contact.numbers {
                add(CallTarget(name: contact.name, number: number.value, label: number.label))
            }
        }
        let phonebook = fritzBox.phonebook.value?.contacts ?? []
        for contact in NameMatcher.matches(name, in: phonebook, name: \.name) {
            for number in contact.numbers {
                add(CallTarget(name: contact.name, number: number.number, label: nil))
            }
        }
        return targets
    }

    /// The last number dialed from this iPhone, for "Wahlwiederholung".
    func lastDialedTarget() -> CallTarget? {
        var descriptor = FetchDescriptor<CallRecord>(
            predicate: #Predicate { $0.directionRaw == "outgoing" },
            sortBy: [SortDescriptor(\.date, order: .reverse)]
        )
        descriptor.fetchLimit = 1
        guard let record = try? modelContainer.mainContext.fetch(descriptor).first else { return nil }
        let name = contacts.name(for: record.number) ?? record.name ?? record.number
        return CallTarget(name: name, number: record.number, label: nil)
    }
}
