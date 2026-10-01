import HousephoneKit
import SwiftUI

/// "Verwaltung" in Settings (ADR-0009): only on admin iPhones, only usable
/// in the home network, behind Face ID.
struct AdminSection: View {
    @Environment(AdminCenter.self) private var admin
    @Environment(BridgeConnection.self) private var bridge

    var body: some View {
        if admin.isAdmin {
            Section {
                content
            } header: {
                Text("Verwaltung")
            } footer: {
                footer
            }
            .onChange(of: bridge.adminRole) { _, _ in admin.syncLocalKey() }
        }
    }

    @ViewBuilder
    private var content: some View {
        if admin.isReady {
            NavigationLink {
                AdminView()
            } label: {
                Label("Bridge verwalten", systemImage: "person.badge.shield.checkmark")
            }
            .disabled(!admin.isReachable)
        } else if admin.canEnroll {
            Button {
                Task { await admin.enroll() }
            } label: {
                HStack {
                    Label("Face ID für die Verwaltung einrichten", systemImage: "faceid")
                    Spacer()
                    if admin.phase == .working { ProgressView() }
                }
            }
            .disabled(!admin.isReachable || admin.phase == .working)
        } else {
            LabeledContent("Face ID") {
                StatusIndicator(tone: .warning, label: "Neu freischalten lassen")
            }
        }
        if case .failed(let message) = admin.phase {
            Text(message)
                .font(.footnote)
                .foregroundStyle(Theme.dangerText)
        }
    }

    @ViewBuilder
    private var footer: some View {
        if !admin.isReachable {
            Text("Nur im Heim-WLAN oder über Tailscale. Unterwegs ist die Verwaltung ausgeblendet.")
        } else if admin.isReady {
            Text("Jede Änderung bestätigst du mit Face ID. Alle Geräte sehen, was ein Admin ändert.")
        } else if admin.canEnroll, let until = admin.role?.enrollUntil {
            Text("Dieses iPhone ist Admin. Richte Face ID bis \(until, format: .dateTime.hour().minute()) ein; danach geht es nur mit einer neuen Freigabe.")
        } else {
            Text("Face ID für die Verwaltung fehlt oder wurde geändert. Mach dieses iPhone auf dem Server erneut zum Admin (TUI, Dashboard oder „devices promote“) und richte Face ID dann neu ein.")
        }
    }
}
