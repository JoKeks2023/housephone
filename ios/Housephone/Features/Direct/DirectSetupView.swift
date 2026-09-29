import HousephoneKit
import SwiftUI

/// Sets up (or edits) the mode without bridge: the IP-phone account on
/// the FRITZ!Box, the home Wi-Fi, optional TR-064 — and says plainly what
/// this mode cannot do.
struct DirectSetupView: View {
    @Environment(DirectPhone.self) private var direct
    @Environment(\.dismiss) private var dismiss
    @State private var configuration: DirectConfiguration
    @State private var address: AddressChoice
    @State private var saveFailed = false

    /// The usual addresses of a FRITZ!Box, plus a way out.
    enum AddressChoice: Hashable {
        case fritzBox
        case defaultIP
        case other

        static let defaultIPAddress = "192.168.178.1"

        init(_ registrar: String) {
            switch registrar {
            case "fritz.box": self = .fritzBox
            case Self.defaultIPAddress: self = .defaultIP
            default: self = .other
            }
        }
    }

    init(configuration: DirectConfiguration = DirectConfiguration()) {
        _configuration = State(initialValue: configuration)
        _address = State(initialValue: AddressChoice(configuration.registrar))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Adresse", selection: $address) {
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

                Section {
                    TextField("WLAN-Name (SSID)", text: $configuration.homeSSID)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } header: {
                    Text("Heim-WLAN")
                } footer: {
                    Text("Nur in diesem WLAN darf Housephone im Hintergrund auf Anrufe warten. Den Namen liest die App nicht selbst aus, dafür bräuchte sie deinen Standort.")
                }

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
                    Button("Sichern") { save() }
                        .disabled(!prepared.isComplete)
                }
            }
            .alert("Nicht gespeichert", isPresented: $saveFailed) {
                Button("OK", role: .cancel) {}
            } message: {
                Text("Die Zugangsdaten konnten nicht im Schlüsselbund gespeichert werden.")
            }
        }
    }

    /// The form's values with the picked address and TR-064 off when empty.
    private var prepared: DirectConfiguration {
        var result = configuration
        switch address {
        case .fritzBox: result.registrar = "fritz.box"
        case .defaultIP: result.registrar = AddressChoice.defaultIPAddress
        case .other: result.registrar = configuration.registrar.trimmingCharacters(in: .whitespaces)
        }
        result.homeSSID = result.homeSSID.trimmingCharacters(in: .whitespaces)
        if !result.usesTR064 {
            result.tr064Username = ""
            result.tr064Password = ""
        }
        return result
    }

    private func save() {
        do {
            try direct.configure(prepared)
            dismiss()
        } catch {
            saveFailed = true
        }
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
