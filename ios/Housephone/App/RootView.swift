import HousephoneKit
import SwiftUI

struct RootView: View {
    @Environment(BridgeConnection.self) private var bridge
    @Environment(CallCenter.self) private var callCenter
    @Environment(AppModel.self) private var appModel
    @Environment(FritzBoxData.self) private var fritzBox

    var body: some View {
        @Bindable var appModel = appModel
        @Bindable var callCenter = callCenter

        Group {
            if bridge.isPaired {
                MainTabView()
            } else {
                OnboardingView()
            }
        }
        .motion(Theme.Motion.standard, value: bridge.isPaired)
        .onChange(of: bridge.isPaired) { _, paired in
            // Phonebook and call list belong to the bridge that is gone.
            if !paired { fritzBox.clear() }
        }
        .task(id: bridge.welcome) {
            // (Re)connected: a fresh phonebook keeps caller names current.
            await fritzBox.refreshAfterConnect()
        }
        .fullScreenCover(isPresented: callScreenPresented) {
            InCallView()
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
            get: { callCenter.activeCall != nil },
            set: { presented in
                // Swiping the cover away is not possible; ending happens via
                // the end button. Nothing to do here.
                _ = presented
            }
        )
    }
}

struct MainTabView: View {
    @Environment(AppModel.self) private var appModel

    var body: some View {
        @Bindable var appModel = appModel

        TabView(selection: $appModel.selectedTab) {
            Tab("Anrufe", systemImage: "clock", value: AppModel.Tab.recents) {
                RecentsView()
            }
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
    }
}
