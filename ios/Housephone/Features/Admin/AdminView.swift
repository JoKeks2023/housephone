import HousephoneKit
import SwiftUI

/// Managing the bridge (ADR-0009): status, pairing requests from the home
/// network, devices per profile, inviting new devices.
struct AdminView: View {
    @Environment(AdminCenter.self) private var admin
    @Environment(\.scenePhase) private var scenePhase
    @State private var showsInvite = false
    @State private var approving: AdminLanRequest?

    var body: some View {
        List {
            if case .failed(let message) = admin.phase {
                Section {
                    Label(message, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(Theme.dangerText)
                        .font(.subheadline)
                }
            }
            if !admin.requests.isEmpty {
                requestsSection
            }
            if let status = admin.status {
                statusSection(status)
            }
            devicesSections
            Section {
                Button {
                    showsInvite = true
                } label: {
                    Label("Neues Gerät einladen", systemImage: "qrcode")
                }
            } footer: {
                Text("Erzeugt einen QR-Code zum Koppeln, gültig für wenige Minuten.")
            }
        }
        .appBackground(.grouped)
        .navigationTitle("Verwaltung")
        .overlay {
            if admin.status == nil, admin.phase == .working {
                ProgressView()
            }
        }
        .refreshable { await admin.refresh() }
        .task { await admin.refresh() }
        .onDisappear { admin.lock() }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { admin.lock() }
        }
        .sheet(isPresented: $showsInvite) {
            AdminInviteView()
        }
        .sheet(item: $approving) { request in
            AdminApproveView(request: request)
                .presentationDetents([.medium, .large])
        }
    }

    // MARK: - Sections

    private var requestsSection: some View {
        Section {
            ForEach(admin.requests) { request in
                Button {
                    approving = request
                } label: {
                    HStack(spacing: Theme.Space.s3) {
                        Image(systemName: "iphone.radiowaves.left.and.right")
                            .foregroundStyle(Theme.accent)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: Theme.Space.s1) {
                            Text(request.deviceName).font(.headline)
                            Text("Code \(request.groupedSAS) · \(request.ip)")
                                .font(.subheadline.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .accessibilityHidden(true)
                    }
                }
                .buttonStyle(.plain)
            }
        } header: {
            Text("Kopplungsanfragen")
        } footer: {
            Text("Gib eine Anfrage nur frei, wenn das neue Gerät genau diesen Code zeigt.")
        }
    }

    private func statusSection(_ status: AdminStatus) -> some View {
        Section("Status") {
            LabeledContent("Bridge", value: status.bridgeName)
            LabeledContent("Version") {
                Text(status.version).font(.callout.monospaced())
            }
            ForEach(status.profiles ?? []) { profile in
                LabeledContent((status.profiles?.count ?? 0) > 1 ? profile.name : String(localized: "FRITZ!Box")) {
                    StatusIndicator(tone: profile.registered ? .positive : .negative, label: profile.registered ? "Angemeldet" : "Nicht angemeldet")
                }
            }
            if let ip = status.publicIp, !ip.isEmpty {
                LabeledContent("Öffentliche IP") {
                    Text(verbatim: "\(ip) (\(status.publicIpSource ?? "–"))")
                        .font(.callout.monospaced())
                        .textSelection(.enabled)
                }
            }
            LabeledContent("Push (APNs)") {
                StatusIndicator(tone: status.apnsConfigured ? .positive : .warning, label: pushLabel(status))
            }
            LabeledContent("Geräte online", value: "\(status.devicesOnline) / \(status.devicesTotal)")
            LabeledContent("Laufende Anrufe", value: "\(status.activeCalls)")
            if let today = admin.stats?.today {
                LabeledContent("Heute") {
                    Text("\(today.incoming) eingehend · \(today.missed) verpasst · \(today.outgoing) ausgehend")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    /// How the bridge sends pushes (ADR-0010); older bridges only say whether.
    private func pushLabel(_ status: AdminStatus) -> LocalizedStringKey {
        switch status.pushMode {
        case "relay": "Über Relay"
        case "apns": "Eigener Schlüssel"
        default: status.apnsConfigured ? "Eingerichtet" : "Fehlt"
        }
    }

    @ViewBuilder
    private var devicesSections: some View {
        let groups = Dictionary(grouping: admin.devices, by: \.profile)
        let order = admin.profiles.map(\.id) + groups.keys.filter { id in !admin.profiles.contains { $0.id == id } }.sorted()
        ForEach(order, id: \.self) { profileID in
            if let devices = groups[profileID], !devices.isEmpty {
                Section {
                    ForEach(devices) { device in
                        NavigationLink {
                            AdminDeviceView(deviceID: device.id)
                        } label: {
                            AdminDeviceRow(device: device)
                        }
                    }
                } header: {
                    if admin.multiProfile {
                        Text(devices.first?.profileName ?? profileID)
                    } else {
                        Text("Geräte")
                    }
                }
            }
        }
    }
}

struct AdminDeviceRow: View {
    let device: AdminDevice

    var body: some View {
        HStack(spacing: Theme.Space.s3) {
            Image(systemName: device.isWatch ? "applewatch" : "iphone")
                .frame(width: 24)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: Theme.Space.s1) {
                HStack(spacing: Theme.Space.s2) {
                    Text(device.name)
                    if device.isAdmin {
                        Text("Admin")
                            .font(.caption.weight(.semibold))
                            .padding(.horizontal, Theme.Space.s2)
                            .padding(.vertical, 2)
                            .background(Theme.accent.opacity(0.15), in: .capsule)
                    }
                }
                Group {
                    if device.online {
                        Text("Online")
                    } else if let lastSeen = device.lastSeen {
                        Text("Zuletzt \(lastSeen, format: .relative(presentation: .named))")
                    } else {
                        Text("Noch nie verbunden")
                    }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// Approving a pairing request: compare the code, choose the profile.
struct AdminApproveView: View {
    @Environment(AdminCenter.self) private var admin
    @Environment(\.dismiss) private var dismiss
    let request: AdminLanRequest
    @State private var profileID = BridgeProfile.defaultID

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(spacing: Theme.Space.s2) {
                        Text(request.groupedSAS)
                            .font(.system(size: 44, weight: .semibold, design: .rounded).monospacedDigit())
                            .accessibilityLabel(Text(request.sas.map(String.init).joined(separator: " ")))
                        Text("Zeigt „\(request.deviceName)“ genau diesen Code?")
                            .multilineTextAlignment(.center)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, Theme.Space.s3)
                }
                Section {
                    LabeledContent("Modell", value: request.model ?? "–")
                    LabeledContent("Adresse") {
                        Text(request.ip).font(.callout.monospaced())
                    }
                    if admin.multiProfile {
                        Picker("Profil", selection: $profileID) {
                            ForEach(admin.profiles) { profile in
                                Text(profile.name).tag(profile.id)
                            }
                        }
                    }
                }
                Section {
                    Button("Code stimmt – freigeben") {
                        Task {
                            await admin.approve(request, profile: admin.profiles.first { $0.id == profileID })
                            dismiss()
                        }
                    }
                    Button("Ablehnen", role: .destructive) {
                        Task {
                            await admin.deny(request)
                            dismiss()
                        }
                    }
                }
            }
            .navigationTitle("Kopplungsanfrage")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Abbrechen") { dismiss() }
                }
            }
            .onAppear {
                profileID = admin.profiles.first?.id ?? BridgeProfile.defaultID
            }
        }
    }
}
