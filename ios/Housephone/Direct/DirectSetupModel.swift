import Foundation
import HousephoneKit
import Observation
import UIKit

/// The automatic part of the direct setup: find the FRITZ!Box (#30), read
/// its numbers and IP phones, and create the IP phone for this iPhone
/// (HPHN-25), including the confirmation at the box.
@MainActor
@Observable
final class DirectSetupModel {
    enum Discovery: Equatable {
        case searching
        case found(FritzBoxDevice)
        case notFound
    }

    enum Survey: Equatable {
        case idle
        case loading
        case ready(FritzBoxPhoneSetup.Survey)
        case failed(String)
    }

    enum Creation: Equatable {
        case idle
        case working
        /// Waiting for the user to confirm at the FRITZ!Box.
        case confirming([FritzBoxConfirmationMethod])
        case failed(String)
    }

    private(set) var discovery: Discovery = .searching
    private(set) var survey: Survey = .idle
    private(set) var creation: Creation = .idle

    @ObservationIgnored private var creationTask: Task<DirectConfiguration?, Never>?

    /// The name of the IP phone in the FRITZ!Box's list of telephony devices.
    static var phoneName: String {
        "Housephone (\(UIDevice.current.name))"
    }

    func discover() async {
        discovery = .searching
        if let device = await FritzBoxFinder.find() {
            discovery = .found(device)
        } else {
            discovery = .notFound
        }
    }

    /// Logs in with the FRITZ!Box user and reads numbers and IP phones.
    func loadSurvey(host: String, username: String, password: String, clientID: String?) async {
        survey = .loading
        let setup = FritzBoxPhoneSetup(service: TR064Client(host: host, username: username, password: password))
        do {
            survey = .ready(try await setup.survey(clientID: clientID, phoneName: Self.phoneName))
        } catch {
            survey = .failed(Self.message(for: error))
        }
    }

    func resetSurvey() {
        survey = .idle
    }

    /// Creates (or reuses) the IP phone and returns the configuration to
    /// save: SIP account plus TR-064 with the same login. Nil when it
    /// failed or was cancelled; `creation` says why.
    func create(
        base: DirectConfiguration,
        host: String,
        username: String,
        password: String,
        reuse: Bool,
        outgoing: FritzBoxLineNumber?,
        incoming: FritzBoxLineNumber?
    ) async -> DirectConfiguration? {
        guard case .ready(let survey) = survey else { return nil }
        creation = .working
        let clientID = reuse ? (survey.existing?.clientID.nonEmpty ?? Self.newClientID()) : Self.newClientID()
        let request: FritzBoxSIPClientRequest
        do {
            request = try FritzBoxPhoneSetup.request(
                survey: survey,
                reuse: reuse,
                phoneName: Self.phoneName,
                clientID: clientID,
                outgoingNumber: outgoing?.number ?? "",
                incomingNumbers: incoming.map { [$0] } ?? []
            )
        } catch {
            creation = .failed(Self.message(for: error))
            return nil
        }

        let setup = FritzBoxPhoneSetup(service: TR064Client(host: host, username: username, password: password))
        let task = Task { [weak self] () -> DirectConfiguration? in
            do {
                _ = try await setup.apply(request) { challenge in
                    await MainActor.run { self?.creation = .confirming(challenge.methods) }
                }
            } catch {
                await MainActor.run { self?.creation = .failed(Self.message(for: error)) }
                return nil
            }
            var configuration = base
            configuration.registrar = host
            configuration.sipUsername = request.username
            configuration.sipPassword = request.password
            configuration.usesTR064 = true
            configuration.tr064Username = username
            configuration.tr064Password = password
            configuration.fritzBoxClientID = request.clientID
            return configuration
        }
        creationTask = task
        let result = await task.value
        creationTask = nil
        if result != nil { creation = .idle }
        return result
    }

    func cancelCreation() {
        creationTask?.cancel()
    }

    func dismissFailure() {
        if case .failed = creation { creation = .idle }
    }

    /// Unique per setup; the FRITZ!Box matches client IDs by substring,
    /// so it must not be a prefix of another one.
    static func newClientID() -> String {
        "housephone-" + UUID().uuidString.lowercased()
    }

    nonisolated static func message(for error: any Error) -> String {
        switch error {
        case FritzBoxPhoneSetup.Failure.noFreeSlot:
            return String(localized: "Alle zehn Plätze für IP-Telefone sind belegt. Lösche in der FRITZ!Box ein IP-Telefon, das du nicht mehr brauchst.")
        case FritzBoxPhoneSetup.Failure.notConfirmed:
            return String(localized: "Die Bestätigung an der FRITZ!Box kam nicht rechtzeitig. Versuche es noch einmal.")
        case FritzBoxPhoneSetup.Failure.tr064(let error):
            return message(for: error)
        case let error as TR064Error:
            switch error {
            case .unreachable: return String(localized: "Die FRITZ!Box antwortet nicht. Bist du im Heim-WLAN?")
            case .authentication: return String(localized: "Die FRITZ!Box hat die Anmeldung abgelehnt. Prüfe Benutzer und Kennwort.")
            case .notAllowed: return String(localized: "Dem FRITZ!Box-Benutzer fehlt das Recht „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“.")
            case .secondFactorBlocked: return String(localized: "Zu viele Bestätigungsversuche. Die FRITZ!Box lässt erst in bis zu einer Stunde wieder einen zu.")
            case .secondFactorBusy: return String(localized: "An der FRITZ!Box läuft gerade eine andere Bestätigung. Versuche es in zwei Minuten noch einmal.")
            case .secondFactorRequired: return String(localized: "Die FRITZ!Box verlangt eine Bestätigung. Versuche es noch einmal.")
            case .unsupported, .invalidResponse: return String(localized: "Die FRITZ!Box hat unerwartet geantwortet.")
            }
        default:
            return String(localized: "Die FRITZ!Box hat unerwartet geantwortet.")
        }
    }
}

private extension String {
    var nonEmpty: String? { isEmpty ? nil : self }
}
