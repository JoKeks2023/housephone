import HousephoneKit
import SwiftData
import SwiftUI

struct RootView: View {
    @Environment(BridgeConnection.self) private var bridge
    @Environment(DirectPhone.self) private var direct
    @Environment(CallCenter.self) private var callCenter
    @Environment(AppModel.self) private var appModel
    @Environment(FritzBoxData.self) private var fritzBox

    var body: some View {
        @Bindable var appModel = appModel
        @Bindable var callCenter = callCenter

        Group {
            if bridge.isPaired || direct.isEnabled {
                MainTabView()
            } else {
                OnboardingView()
            }
        }
        .motion(Theme.Motion.standard, value: bridge.isPaired || direct.isEnabled)
        .onChange(of: bridge.isPaired) { _, paired in
            // Phonebook and call list belong to the bridge that is gone.
            if !paired { fritzBox.clear() }
        }
        .onChange(of: direct.isEnabled) { _, enabled in
            if !enabled { fritzBox.clear() }
        }
        .task(id: bridge.welcome) {
            // (Re)connected: a fresh phonebook keeps caller names current.
            await fritzBox.refreshAfterConnect()
        }
        .task(id: direct.status == .ready) {
            if direct.status == .ready { await fritzBox.refreshAfterConnect() }
        }
        .fullScreenCover(isPresented: callScreenPresented) {
            InCallView()
                // A call never goes away by accident; it ends or minimizes
                // with its own buttons.
                .interactiveDismissDisabled(true)
        }
        .onChange(of: callCenter.activeCall?.id) { _, _ in
            appModel.isCallMinimized = false
        }
        .sheet(item: $appModel.pairingLink) { link in
            PairingView(link: link)
        }
        .alert(
            Text("Link nicht erkannt"),
            isPresented: Binding(
                get: { appModel.pairingLinkError != nil },
                set: { if !$0 { appModel.pairingLinkError = nil } }
            ),
            presenting: appModel.pairingLinkError
        ) { _ in
            Button("OK", role: .cancel) {}
        } message: { error in
            Text(error.message)
        }
        .alert(
            Text("Anruf nicht möglich"),
            isPresented: Binding(
                get: { callCenter.failure != nil },
                set: { if !$0 { callCenter.failure = nil } }
            ),
            presenting: callCenter.failure
        ) { _ in
            Button("OK", role: .cancel) {}
        } message: { failure in
            Text(failure.message)
        }
    }

    private var callScreenPresented: Binding<Bool> {
        Binding(
            get: { callCenter.activeCall != nil && !appModel.isCallMinimized },
            set: { presented in
                // Interactive dismissal is disabled; the only way out while
                // the call runs is the minimize button.
                if !presented, callCenter.activeCall != nil { appModel.isCallMinimized = true }
            }
        )
    }
}

/// The running call as a glass pill above the tab bar; tap to return.
private struct CallPill: View {
    @Environment(CallCenter.self) private var callCenter
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(AppModel.self) private var appModel

    var body: some View {
        if let call = callCenter.activeCall {
            Button {
                appModel.isCallMinimized = false
            } label: {
                HStack(spacing: Theme.Space.s2) {
                    Image(systemName: "phone.fill")
                        .foregroundStyle(Theme.call)
                        .accessibilityHidden(true)
                    Text(contacts.name(for: call.remoteNumber) ?? call.remoteName ?? (call.remoteNumber.isEmpty ? String(localized: "Unbekannt") : call.remoteNumber))
                        .font(.subheadline.weight(.medium))
                        .lineLimit(1)
                    Spacer(minLength: Theme.Space.s2)
                    if call.phase == .connected, let connectedAt = call.connectedAt {
                        Text(timerInterval: connectedAt...Date.distantFuture, countsDown: false)
                            .font(.subheadline)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.horizontal, Theme.Space.s4)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text("Zurück zum Anruf"))
        }
    }
}

struct MainTabView: View {
    @Environment(AppModel.self) private var appModel
    @Environment(CallCenter.self) private var callCenter

    @Query(
        filter: #Predicate<CallRecord> { $0.directionRaw == "incoming" && $0.outcomeRaw == "missed" },
        sort: \CallRecord.date,
        order: .reverse
    )
    private var missedCalls: [CallRecord]

    /// Missed calls since the recents were last looked at, as the tab
    /// badge (like the Phone app).
    private var newMissed: Int {
        missedCalls.prefix { $0.date > appModel.recentsSeenAt }.count
    }

    var body: some View {
        if #available(iOS 26.1, *) {
            tabs.tabViewBottomAccessory(isEnabled: appModel.isCallMinimized && callCenter.activeCall != nil) {
                CallPill()
            }
        } else {
            tabs
        }
    }

    private var tabs: some View {
        @Bindable var appModel = appModel

        return TabView(selection: $appModel.selectedTab) {
            Tab("Anrufe", systemImage: "clock", value: AppModel.Tab.recents) {
                RecentsView()
            }
            .badge(newMissed)
            Tab("Tastenfeld", systemImage: "circle.grid.3x3.fill", value: AppModel.Tab.keypad) {
                KeypadView()
            }
            Tab("Kontakte", systemImage: "person.crop.circle", value: AppModel.Tab.contacts) {
                ContactsView()
            }
            Tab("Einstellungen", systemImage: "gearshape", value: AppModel.Tab.settings) {
                SettingsView()
            }
        }
        .tabBarMinimizeBehavior(.onScrollDown)
        .devicePairedBanner()
        .adminActionBanner()
    }
}
