import HousephoneKit
import SwiftUI

/// Sets up (or edits) the mode without bridge. Finds the FRITZ!Box by
/// itself (#30) and, with one FRITZ!Box login, creates the IP phone and
/// switches on phonebook and call list (HPHN-25). Entering an existing IP
/// phone by hand stays possible. Says plainly what this mode cannot do.
struct DirectSetupView: View {
    @Environment(DirectPhone.self) private var direct
    @Environment(\.dismiss) private var dismiss
    @State private var configuration: DirectConfiguration
    @State private var address: AddressChoice
    @State private var method: SetupMethod
    @State private var model = DirectSetupModel()
    @State private var outgoing: FritzBoxLineNumber?
    @State private var incoming: FritzBoxLineNumber?
    @State private var reuseExisting = true
    @State private var saveFailed = false

    /// Where the FRITZ!Box is: the one found on the Wi-Fi, the usual
    /// addresses, or something else.
    enum AddressChoice: Hashable {
        case found
        case fritzBox
        case defaultIP
        case other

        static let defaultIPAddress = FritzBoxDiscovery.factoryAddress

        init(_ registrar: String) {
            switch registrar {
            case "fritz.box": self = .fritzBox
            case Self.defaultIPAddress: self = .defaultIP
            default: self = .other
            }
        }
    }

    enum SetupMethod: Hashable {
        /// Housephone creates the IP phone over TR-064.
        case automatic
        /// The user enters an IP phone created in the FRITZ!Box.
        case manual
    }

    /// New setup: automatic and with the box found on the Wi-Fi. Editing:
    /// the stored values, by hand.
    init(configuration: DirectConfiguration? = nil) {
        let initial = configuration ?? DirectConfiguration()
        _configuration = State(initialValue: initial)
        _address = State(initialValue: configuration == nil ? .found : AddressChoice(initial.registrar))
        _method = State(initialValue: configuration == nil ? .automatic : .manual)
    }

    var body: some View {
        NavigationStack {
            Form {
                fritzBoxSection
                methodSection
                if method == .automatic {
                    loginSection
                    automaticSection
                } else {
                    manualSection
                }
                wifiSection
                if method == .manual {
                    tr064Section
                }
                Section {
                    DirectLimitsList()
                } header: {
                    Text("Das geht ohne Bridge nicht")
                }
            }
            .navigationTitle("Direkt mit FRITZ!Box")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Abbrechen", role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        save()
                    } label: {
                        if method == .automatic { Text("Einrichten") } else { Text("Sichern") }
                    }
                    .disabled(!canSave)
                }
            }
            .task { await model.discover() }
            .onChange(of: model.discovery) { _, discovery in
                // Nothing found: fall back to the usual name.
                if discovery == .notFound, address == .found { address = .fritzBox }
            }
            .onChange(of: host) { _, _ in model.resetSurvey() }
            .sheet(isPresented: confirming) {
                ConfirmAtFritzBoxSheet(methods: confirmationMethods) {
                    model.cancelCreation()
                }
                .presentationDetents([.medium])
                .interactiveDismissDisabled(true)
            }
            .alert("Nicht eingerichtet", isPresented: creationFailed) {
                Button("OK", role: .cancel) { model.dismissFailure() }
            } message: {
                if case .failed(let message) = model.creation { Text(message) }
            }
            .alert("Nicht gespeichert", isPresented: $saveFailed) {
                Button("OK", role: .cancel) {}
            } message: {
                Text("Die Zugangsdaten konnten nicht im Schlüsselbund gespeichert werden.")
            }
        }
    }

    // MARK: - Sections

    @ViewBuilder private var fritzBoxSection: some View {
        Section {
            switch model.discovery {
            case .searching:
                HStack(spacing: Theme.Space.s3) {
                    ProgressView()
                    Text("Suche FRITZ!Box im WLAN …").foregroundStyle(.secondary)
                }
            case .found(let device):
                FoundFritzBoxRow(device: device)
            case .notFound:
                Label {
                    Text("Keine FRITZ!Box gefunden. Bist du im Heim-WLAN?")
                } icon: {
                    Image(systemName: "wifi.exclamationmark").foregroundStyle(Theme.warningText)
                }
                Button("Erneut suchen") { Task { await model.discover() } }
            }
            Picker("Adresse", selection: $address) {
                if case .found(let device) = model.discovery {
                    Text(verbatim: device.host).tag(AddressChoice.found)
                }
                Text(verbatim: "fritz.box").tag(AddressChoice.fritzBox)
                Text(verbatim: AddressChoice.defaultIPAddress).tag(AddressChoice.defaultIP)
                Text("Andere …").tag(AddressChoice.other)
            }
            if address == .other {
                TextField("Host-Name oder IP-Adresse", text: $configuration.registrar)
                    .textContentType(.URL)
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
        } header: {
            Text("FRITZ!Box")
        }
    }

    private var methodSection: some View {
        Section {
            Picker("Einrichtung", selection: $method) {
                Text("Automatisch").tag(SetupMethod.automatic)
                Text("Manuell").tag(SetupMethod.manual)
            }
            .pickerStyle(.segmented)
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets())
        } footer: {
            if method == .automatic {
                Text("Housephone legt sich mit deiner FRITZ!Box-Anmeldung selbst als IP-Telefon an und lädt Telefonbuch und Anrufliste.")
            } else {
                Text("Du trägst ein IP-Telefon ein, das du in der FRITZ!Box selbst angelegt hast.")
            }
        }
    }

    private var loginSection: some View {
        Section {
            TextField("FRITZ!Box-Benutzer", text: $configuration.tr064Username)
                .textContentType(.username)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            SecureField("Kennwort", text: $configuration.tr064Password)
                .textContentType(.password)
            if needsSurvey {
                Button {
                    Task { await loadSurvey() }
                } label: {
                    if model.survey == .loading {
                        HStack(spacing: Theme.Space.s2) {
                            ProgressView()
                            Text("Verbinde …")
                        }
                    } else {
                        Text("Mit FRITZ!Box verbinden")
                    }
                }
                .disabled(host.isEmpty || configuration.tr064Password.isEmpty || model.survey == .loading)
            }
            if case .failed(let message) = model.survey {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(Theme.dangerText)
            }
        } header: {
            Text("Anmeldung an der FRITZ!Box")
        } footer: {
            Text("Ein FRITZ!Box-Benutzer mit dem Recht „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“. Die Daten bleiben im Schlüsselbund dieses iPhones.")
        }
    }

    @ViewBuilder private var automaticSection: some View {
        if case .ready(let survey) = model.survey {
            if let existing = survey.existing {
                Section {
                    Picker("Vorhandenes IP-Telefon", selection: $reuseExisting) {
                        Text("Weiterverwenden").tag(true)
                        Text("Neu anlegen").tag(false)
                    }
                } header: {
                    Text("Schon eingerichtet")
                } footer: {
                    Text("„\(existing.phoneName)“ (\(existing.internalNumber)) gibt es schon. Beim Weiterverwenden bekommt es ein neues Kennwort.")
                }
            }
            if !survey.hasFreeSlot && !(reuseExisting && survey.existing != nil) {
                Section {
                    Label {
                        Text("Alle zehn Plätze für IP-Telefone sind belegt. Lösche in der FRITZ!Box ein IP-Telefon, das du nicht mehr brauchst.")
                    } icon: {
                        Image(systemName: "exclamationmark.triangle").foregroundStyle(Theme.warningText)
                    }
                }
            }
            Section {
                Picker("Ausgehend", selection: $outgoing) {
                    Text("Automatisch").tag(FritzBoxLineNumber?.none)
                    ForEach(survey.numbers) { number in
                        NumberLabel(number: number).tag(Optional(number))
                    }
                }
                Picker("Klingelt bei", selection: $incoming) {
                    Text("Allen Nummern").tag(FritzBoxLineNumber?.none)
                    ForEach(survey.numbers) { number in
                        NumberLabel(number: number).tag(Optional(number))
                    }
                }
            } header: {
                Text("Rufnummern")
            } footer: {
                Text("„Automatisch“ nimmt die erste Nummer der FRITZ!Box.")
            }
        }
    }

    private var manualSection: some View {
        Section {
            TextField("Benutzername", text: $configuration.sipUsername)
                .textContentType(.username)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            SecureField("Kennwort", text: $configuration.sipPassword)
                .textContentType(.password)
        } header: {
            Text("IP-Telefon")
        } footer: {
            Text("Lege in der FRITZ!Box unter Telefonie → Telefoniegeräte → Neues Gerät einrichten ein „Telefon (mit und ohne Anrufbeantworter)“ als „LAN/WLAN (IP-Telefon)“ an und trage hier dessen Benutzernamen und Kennwort ein.")
        }
    }

    private var wifiSection: some View {
        Section {
            TextField("WLAN-Name (SSID)", text: $configuration.homeSSID)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        } header: {
            Text("Heim-WLAN")
        } footer: {
            Text("Nur in diesem WLAN darf Housephone im Hintergrund auf Anrufe warten. Den Namen liest die App nicht selbst aus, dafür bräuchte sie deinen Standort.")
        }
    }

    private var tr064Section: some View {
        Section {
            Toggle("Von der FRITZ!Box laden", isOn: $configuration.usesTR064)
            if configuration.usesTR064 {
                TextField("FRITZ!Box-Benutzer", text: $configuration.tr064Username)
                    .textContentType(.username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                SecureField("Kennwort", text: $configuration.tr064Password)
                    .textContentType(.password)
            }
        } header: {
            Text("Telefonbuch & Anrufliste")
        } footer: {
            Text("Optional. Nutze einen eigenen FRITZ!Box-Benutzer mit dem Recht „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“.")
        }
    }

    // MARK: - State

    /// The registrar the choice stands for.
    private var host: String {
        switch address {
        case .found:
            if case .found(let device) = model.discovery { device.host } else { "" }
        case .fritzBox: "fritz.box"
        case .defaultIP: AddressChoice.defaultIPAddress
        case .other: configuration.registrar.trimmingCharacters(in: .whitespaces)
        }
    }

    private var needsSurvey: Bool {
        if case .ready = model.survey { false } else { true }
    }

    private var canSave: Bool {
        switch method {
        case .manual:
            return prepared.isComplete
        case .automatic:
            guard case .ready(let survey) = model.survey, model.creation == .idle else { return false }
            return survey.hasFreeSlot || (reuseExisting && survey.existing != nil)
        }
    }

    private var confirming: Binding<Bool> {
        Binding(
            get: { if case .confirming = model.creation { true } else { false } },
            set: { if !$0 { model.cancelCreation() } }
        )
    }

    private var confirmationMethods: [FritzBoxConfirmationMethod] {
        if case .confirming(let methods) = model.creation { methods } else { [] }
    }

    private var creationFailed: Binding<Bool> {
        Binding(
            get: { if case .failed = model.creation { true } else { false } },
            set: { if !$0 { model.dismissFailure() } }
        )
    }

    /// The form's values with the picked address and TR-064 off when empty.
    private var prepared: DirectConfiguration {
        var result = configuration
        result.registrar = host
        result.homeSSID = result.homeSSID.trimmingCharacters(in: .whitespaces)
        if !result.usesTR064 {
            result.tr064Username = ""
            result.tr064Password = ""
        }
        return result
    }

    private func loadSurvey() async {
        await model.loadSurvey(
            host: host,
            username: configuration.tr064Username.trimmingCharacters(in: .whitespaces),
            password: configuration.tr064Password,
            clientID: configuration.fritzBoxClientID
        )
        if case .ready(let survey) = model.survey, let existing = survey.existing {
            reuseExisting = true
            outgoing = survey.numbers.first { $0.number == existing.outgoingNumber }
            incoming = existing.incomingNumbers.count == 1 ? survey.numbers.first { $0.number == existing.incomingNumbers[0].number } : nil
        }
    }

    private func save() {
        switch method {
        case .manual:
            store(prepared)
        case .automatic:
            Task {
                var base = configuration
                base.homeSSID = base.homeSSID.trimmingCharacters(in: .whitespaces)
                let created = await model.create(
                    base: base,
                    host: host,
                    username: configuration.tr064Username.trimmingCharacters(in: .whitespaces),
                    password: configuration.tr064Password,
                    reuse: reuseExisting,
                    outgoing: outgoing,
                    incoming: incoming
                )
                if let created { store(created) }
            }
        }
    }

    private func store(_ configuration: DirectConfiguration) {
        do {
            try direct.configure(configuration)
            dismiss()
        } catch {
            saveFailed = true
        }
    }
}

/// The FRITZ!Box found on the Wi-Fi, as a confident headline.
private struct FoundFritzBoxRow: View {
    let device: FritzBoxDevice

    var body: some View {
        HStack(spacing: Theme.Space.s3) {
            Image(systemName: "checkmark.circle.fill")
                .font(.title2)
                .foregroundStyle(Theme.call)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: Theme.Space.hairline) {
                Text("\(device.modelName) gefunden")
                    .font(.headline)
                Group {
                    if let version = device.osVersion {
                        Text("FRITZ!OS \(version) · \(device.host)")
                    } else {
                        Text(verbatim: device.host)
                    }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, Theme.Space.s1)
        .accessibilityElement(children: .combine)
    }
}

/// A number of the FRITZ!Box with its name, if it has one.
private struct NumberLabel: View {
    let number: FritzBoxLineNumber

    var body: some View {
        if number.name.isEmpty {
            Text(verbatim: number.number)
        } else {
            Text(verbatim: "\(number.number) · \(number.name)")
        }
    }
}

/// Shown while the FRITZ!Box waits for the user to confirm the change
/// (two-factor confirmation of FRITZ!OS, X_AVM-DE_Auth).
private struct ConfirmAtFritzBoxSheet: View {
    let methods: [FritzBoxConfirmationMethod]
    let onCancel: () -> Void

    var body: some View {
        VStack(spacing: Theme.Space.s5) {
            Image(systemName: "hand.tap")
                .font(.system(size: 44))
                .foregroundStyle(Theme.accentText)
                .accessibilityHidden(true)
            Text("Bestätige an der FRITZ!Box")
                .font(.title3.weight(.semibold))
            VStack(alignment: .leading, spacing: Theme.Space.s3) {
                ForEach(methods, id: \.self) { method in
                    switch method {
                    case .button:
                        Label("Drücke eine beliebige Taste an der FRITZ!Box.", systemImage: "button.programmable")
                    case .dtmf(let code):
                        Label {
                            Text("Oder wähle an einem Telefon, das an der FRITZ!Box angeschlossen ist: \(Text(verbatim: code).font(.body.monospaced().weight(.semibold)))")
                        } icon: {
                            Image(systemName: "phone")
                        }
                    }
                }
                if methods.isEmpty {
                    Label("Folge dem Hinweis an der FRITZ!Box.", systemImage: "info.circle")
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: Theme.Space.s2) {
                ProgressView()
                Text("Warte auf Bestätigung …").foregroundStyle(.secondary)
            }
            .font(.subheadline)
            Button("Abbrechen", role: .cancel, action: onCancel)
                .buttonStyle(.bordered)
        }
        .padding(Theme.Space.s6)
    }
}

/// What the mode without bridge cannot do, in plain words.
struct DirectLimitsList: View {
    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s4) {
            limit(symbol: "house", title: "Nur im Heim-WLAN", detail: "Unterwegs bist du nicht erreichbar und kannst nicht anrufen.")
            #if HOUSEPHONE_LOCAL_PUSH
            limit(symbol: "bell.badge", title: "Klingeln im Hintergrund nur zu Hause", detail: "Außerhalb deines Heim-WLANs wartet Housephone nicht auf Anrufe.")
            #else
            limit(symbol: "bell.slash", title: "Klingelt nur bei geöffneter App", detail: "Im Hintergrund klingelt es erst, wenn Apple Housephone die Local-Push-Freigabe erteilt hat.")
            #endif
            limit(symbol: "applewatch.slash", title: "Keine Apple Watch", detail: "Die Watch telefoniert nur über eine Bridge.")
            limit(symbol: "waveform", title: "Standardqualität", detail: "G.711 statt HD-Telefonie (G.722).")
        }
        .padding(.vertical, Theme.Space.s1)
    }

    private func limit(symbol: String, title: LocalizedStringKey, detail: LocalizedStringKey) -> some View {
        HStack(alignment: .top, spacing: Theme.Space.s3) {
            Image(systemName: symbol)
                .foregroundStyle(.secondary)
                .frame(width: 24)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.subheadline.weight(.semibold))
                Text(detail).font(.footnote).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
