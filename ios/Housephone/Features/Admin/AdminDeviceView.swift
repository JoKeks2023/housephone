import HousephoneKit
import SwiftUI

/// One device: rename, move to another profile, admin role, remove. Every
/// change asks for Face ID; removing also asks for confirmation.
struct AdminDeviceView: View {
    @Environment(AdminCenter.self) private var admin
    @Environment(BridgeConnection.self) private var bridge
    @Environment(\.dismiss) private var dismiss
    let deviceID: String
    @State private var name = ""
    @State private var confirmsRemove = false
    @State private var confirmsDemote = false

    private var device: AdminDevice? { admin.devices.first { $0.id == deviceID } }
    private var isThisIPhone: Bool { bridge.credentials?.deviceId.description == deviceID }
    private var companions: [AdminDevice] { admin.devices.filter { $0.pairedBy == deviceID } }

    var body: some View {
        Form {
            if let device {
                details(device)
            } else {
                Text("Das Gerät gibt es nicht mehr.")
                    .foregroundStyle(.secondary)
            }
        }
        .appBackground(.grouped)
        .navigationTitle(device?.name ?? "")
        .onAppear { name = device?.name ?? "" }
        .confirmationDialog("Gerät entfernen?", isPresented: $confirmsRemove, titleVisibility: .visible) {
            if let device {
                Button("Entfernen", role: .destructive) {
                    Task {
                        await admin.remove(device, keepCompanions: false)
                        dismiss()
                    }
                }
            }
        } message: {
            if companions.isEmpty {
                Text("Die Verbindung wird sofort getrennt. Das Gerät muss neu gekoppelt werden.")
            } else {
                Text("Die Verbindung wird sofort getrennt. Die darüber gekoppelte Apple Watch wird ebenfalls entfernt.")
            }
        }
        .confirmationDialog("Admin-Rechte entziehen?", isPresented: $confirmsDemote, titleVisibility: .visible) {
            if let device {
                Button("Entziehen", role: .destructive) {
                    Task { await admin.demote(device) }
                }
            }
        } message: {
            if isThisIPhone {
                Text("Danach kannst du die Bridge von diesem iPhone aus nicht mehr verwalten.")
            }
        }
    }

    @ViewBuilder
    private func details(_ device: AdminDevice) -> some View {
        Section {
            TextField("Name", text: $name)
                .submitLabel(.done)
                .onSubmit { save(device) }
            if name != device.name, !name.trimmingCharacters(in: .whitespaces).isEmpty {
                Button("Namen speichern") { save(device) }
            }
        } header: {
            Text("Name")
        }

        Section {
            LabeledContent("Gerät", value: device.model ?? (device.isWatch ? "Apple Watch" : "iPhone"))
            if let via = device.pairedByName {
                LabeledContent("Gekoppelt über", value: via)
            }
            LabeledContent("Gekoppelt") {
                Text(device.createdAt, format: .dateTime.day().month().year())
            }
            LabeledContent("Schlüssel") {
                Text(device.keyFingerprint)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
            }
        }

        if admin.multiProfile {
            Section {
                if device.isCompanion {
                    LabeledContent("Profil", value: device.profileName)
                } else {
                    Menu {
                        ForEach(admin.profiles) { profile in
                            Button(profile.name) {
                                Task { await admin.move(device, to: profile) }
                            }
                            .disabled(profile.id == device.profile)
                        }
                    } label: {
                        LabeledContent("Profil", value: device.profileName)
                    }
                }
            } footer: {
                if device.isCompanion {
                    Text("Eine Apple Watch gehört zum Profil ihres iPhones.")
                } else {
                    Text("Beim Verschieben verbindet sich das Gerät neu, seine Apple Watch wechselt mit.")
                }
            }
        }

        if !device.isWatch {
            Section {
                if device.isAdmin {
                    if device.adminEnrolled != true, !isThisIPhone {
                        Button("Face ID neu einrichten lassen") {
                            Task { await admin.promote(device) }
                        }
                    }
                    Button("Admin-Rechte entziehen", role: .destructive) {
                        confirmsDemote = true
                    }
                } else {
                    Button("Zum Admin machen") {
                        Task { await admin.promote(device) }
                    }
                }
            } header: {
                Text("Verwaltung")
            } footer: {
                if device.isAdmin {
                    Text("Admins verwalten die Bridge im Heimnetz mit Face ID.")
                } else {
                    Text("Das iPhone richtet danach innerhalb einer Stunde Face ID für die Verwaltung ein.")
                }
            }
        }

        if !isThisIPhone {
            Section {
                Button("Gerät entfernen", role: .destructive) {
                    confirmsRemove = true
                }
            }
        }
    }

    private func save(_ device: AdminDevice) {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != device.name else { return }
        Task { await admin.rename(device, to: trimmed) }
    }
}
