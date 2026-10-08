import AVFAudio
import HousephoneKit
import SwiftUI

/// Call screen while a call runs; otherwise home or the pairing hint.
struct WatchRootView: View {
    @Environment(WatchBridge.self) private var bridge
    @Environment(WatchCallCenter.self) private var callCenter
    @Environment(WatchFritzBox.self) private var fritzBox
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            if callCenter.activeCall != nil {
                WatchInCallView()
                    .transition(.opacity)
            } else if bridge.isPaired {
                NavigationStack {
                    WatchHomeView()
                }
                .transition(.opacity)
            } else {
                PairingHintView()
                    .transition(.opacity)
            }
        }
        .motion(WatchTheme.Motion.standard, value: callCenter.activeCall?.id)
        .motion(WatchTheme.Motion.standard, value: bridge.isPaired)
        .onChange(of: bridge.isPaired) { _, paired in
            // Phonebook and call list belong to the bridge that is gone.
            if !paired { fritzBox.clear() }
        }
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active, bridge.isPaired else { return }
            Task { await fritzBox.refreshAll() }
        }
        .alert(
            Text("Anruf nicht möglich"),
            isPresented: Binding(
                get: { callCenter.failure != nil },
                set: { if !$0 { callCenter.failure = nil } }
            ),
            presenting: callCenter.failure
        ) { _ in
            Button("OK") { callCenter.failure = nil }
        } message: { failure in
            Text(failure.message)
        }
    }
}

/// Shown until the iPhone has paired the watch with the bridge.
struct PairingHintView: View {
    @Environment(WatchBridge.self) private var bridge

    var body: some View {
        ScrollView {
            VStack(spacing: WatchTheme.Space.s3) {
                Image(systemName: bridge.isPairing ? "applewatch.radiowaves.left.and.right" : "iphone.and.arrow.forward")
                    .font(.system(size: 34, weight: .medium))
                    .foregroundStyle(Color.accentColor)
                    .symbolEffect(.pulse, isActive: bridge.isPairing)
                    .padding(.top, WatchTheme.Space.s2)

                if bridge.isPairing {
                    Text("Wird gekoppelt …")
                        .font(.headline)
                } else if bridge.isRejected {
                    Text("Kopplung ungültig")
                        .font(.headline)
                    Text("Die Bridge kennt diese Watch nicht mehr. Kopple sie auf dem iPhone neu.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                } else {
                    Text("Mit iPhone koppeln")
                        .font(.headline)
                    Text("Öffne Housephone auf dem iPhone und tippe in den Einstellungen auf „Apple Watch koppeln“.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }

                if let failure = bridge.lastFailure, !bridge.isPairing {
                    Label {
                        Text(failure)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(WatchTheme.warning)
                    }
                    .font(.footnote)
                }
            }
            .frame(maxWidth: .infinity)
        }
    }
}

/// Home: readiness at a glance, dial, contacts, missed and recent calls.
struct WatchHomeView: View {
    @Environment(WatchBridge.self) private var bridge
    @Environment(WatchCallCenter.self) private var callCenter
    @Environment(RecentCalls.self) private var recents
    @Environment(WatchFritzBox.self) private var fritzBox
    @Environment(WatchFavorites.self) private var favorites
    @State private var microphone = AVAudioApplication.shared.recordPermission

    var body: some View {
        List {
            Section {
                readiness
                    .listRowBackground(Color.clear)
                NavigationLink {
                    WatchKeypadView()
                } label: {
                    Label("Wählen", systemImage: "circle.grid.3x3.fill")
                        .foregroundStyle(Color.accentColor)
                }
                NavigationLink {
                    WatchContactsView()
                } label: {
                    Label("Kontakte", systemImage: "person.crop.circle")
                }
                if microphone != .granted {
                    Button {
                        Task {
                            _ = await AVAudioApplication.requestRecordPermission()
                            microphone = AVAudioApplication.shared.recordPermission
                        }
                    } label: {
                        Label("Mikrofon erlauben", systemImage: "mic.slash")
                    }
                }
            }

            if !favorites.favorites.isEmpty {
                Section {
                    ForEach(favorites.favorites) { favorite in
                        Button {
                            Task { await callCenter.startCall(to: favorite.number, name: favorite.name) }
                        } label: {
                            VStack(alignment: .leading, spacing: WatchTheme.Space.hairline) {
                                Text(favorite.name)
                                    .lineLimit(1)
                                Text(favorite.label ?? favorite.number)
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                            }
                            .accessibilityElement(children: .combine)
                        }
                        .accessibilityHint(Text("Anrufen"))
                    }
                } header: {
                    Text("Favoriten")
                } footer: {
                    Text("Vom iPhone")
                }
            }

            let missed = fritzBox.recentMissedCalls()
            if !missed.isEmpty {
                Section {
                    ForEach(missed) { call in
                        Button {
                            Task { await callCenter.startCall(to: call.number, name: missedName(call)) }
                        } label: {
                            MissedCallRow(call: call, name: missedName(call))
                        }
                        .disabled(call.number.isEmpty)
                    }
                } header: {
                    Text("Verpasst")
                } footer: {
                    Text("Letzte 24 Stunden, laut FRITZ!Box")
                }
            }

            Section("Zuletzt") {
                if recents.calls.isEmpty {
                    Text("Noch keine Anrufe")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(recents.calls) { call in
                        Button {
                            Task { await callCenter.startCall(to: call.number, name: call.name) }
                        } label: {
                            RecentCallRow(call: call)
                        }
                        .disabled(call.number.isEmpty)
                    }
                }
            }

            Section {
                NavigationLink {
                    WatchPairingView()
                } label: {
                    Label("Kopplung", systemImage: "link")
                }
            }
        }
        .navigationTitle(bridge.bridgeName ?? String(localized: "Housephone"))
        .onAppear {
            microphone = AVAudioApplication.shared.recordPermission
        }
        .task { await fritzBox.refreshAll() }
    }

    private func missedName(_ call: FritzBoxCall) -> String? {
        if let name = fritzBox.name(for: call.number) { return name }
        guard let name = call.name?.trimmingCharacters(in: .whitespaces), !name.isEmpty else { return nil }
        return name
    }

    @ViewBuilder
    private var readiness: some View {
        switch bridge.registration {
        case .registered:
            WatchStatus(tone: .positive, label: "Bereit für Anrufe")
        case .registering:
            WatchStatus(tone: .neutral, label: "Wird eingerichtet …", isBusy: true)
        case .failed:
            WatchStatus(tone: .warning, label: "Bridge nicht erreichbar")
        case .none:
            WatchStatus(tone: .neutral, label: "Noch nicht bereit für Anrufe")
        }
    }
}

/// The bridge this watch is paired with, and the way out: unpairing sits
/// one level deeper so it can't be tapped by accident on the home list.
struct WatchPairingView: View {
    @Environment(WatchBridge.self) private var bridge
    @Environment(WatchCallCenter.self) private var callCenter
    @State private var confirmsUnpair = false
    @State private var isUnpairing = false

    var body: some View {
        List {
            if let name = bridge.bridgeName {
                Section("Bridge") {
                    Text(name)
                }
            }
            Section {
                Button(role: .destructive) {
                    confirmsUnpair = true
                } label: {
                    if isUnpairing {
                        HStack(spacing: WatchTheme.Space.s2) {
                            ProgressView()
                            Text("Wird entkoppelt …")
                        }
                    } else {
                        Label("Kopplung aufheben", systemImage: "link.badge.minus")
                    }
                }
                .disabled(isUnpairing || callCenter.hasActiveCall)
            } footer: {
                Text("Danach klingelt diese Watch nicht mehr. Du kannst sie jederzeit über das iPhone neu koppeln.")
            }
        }
        .navigationTitle("Kopplung")
        .confirmationDialog("Kopplung aufheben?", isPresented: $confirmsUnpair, titleVisibility: .visible) {
            Button("Kopplung aufheben", role: .destructive) {
                isUnpairing = true
                Task {
                    await bridge.unpair()
                    isUnpairing = false
                }
            }
            Button("Abbrechen", role: .cancel) {}
        } message: {
            Text("Die Zugangsdaten werden von dieser Watch gelöscht.")
        }
    }
}

struct RecentCallRow: View {
    let call: RecentCall
    @Environment(WatchFritzBox.self) private var fritzBox

    var body: some View {
        HStack(spacing: WatchTheme.Space.s2) {
            Image(systemName: symbol)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(call.isMissed ? WatchTheme.danger : .secondary)
                .frame(width: 16)
            VStack(alignment: .leading, spacing: WatchTheme.Space.hairline) {
                Text(title)
                    .font(.body)
                    .foregroundStyle(call.isMissed ? WatchTheme.danger : .primary)
                    .lineLimit(1)
                Text(call.date, format: .relative(presentation: .named))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityHint(Text("Zurückrufen"))
    }

    private var title: String {
        if let name = call.name, !name.isEmpty { return name }
        if let name = fritzBox.name(for: call.number) { return name }
        return call.number.isEmpty ? String(localized: "Unbekannt") : call.number
    }

    private var symbol: String {
        switch (call.direction, call.outcome) {
        case (.incoming, .missed): "phone.arrow.down.left.fill"
        case (.incoming, _): "phone.arrow.down.left"
        case (.outgoing, _): "phone.arrow.up.right"
        }
    }
}

/// A missed call from the FRITZ!Box call list.
struct MissedCallRow: View {
    let call: FritzBoxCall
    let name: String?

    var body: some View {
        HStack(spacing: WatchTheme.Space.s2) {
            Image(systemName: "phone.arrow.down.left.fill")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(WatchTheme.danger)
                .frame(width: 16)
            VStack(alignment: .leading, spacing: WatchTheme.Space.hairline) {
                Text(name ?? (call.number.isEmpty ? String(localized: "Unbekannt") : call.number))
                    .foregroundStyle(WatchTheme.danger)
                    .lineLimit(1)
                Text(call.startedAt, format: .relative(presentation: .named))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityHint(Text("Zurückrufen"))
    }
}
