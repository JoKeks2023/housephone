import HousephoneKit
import SwiftUI

extension View {
    /// Shows what an admin changed at the bridge (ADR-0009: every device
    /// sees every admin action).
    func adminActionBanner() -> some View {
        modifier(AdminActionBannerModifier())
    }
}

private struct AdminActionBannerModifier: ViewModifier {
    @Environment(BridgeConnection.self) private var bridge

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .top) {
                // A new pairing has its own banner; it wins.
                if let action = bridge.lastAdminAction, bridge.newlyPairedDevice == nil {
                    AdminActionBanner(action: action) {
                        bridge.dismissAdminAction()
                    }
                    .padding(.horizontal, Theme.Space.s4)
                    .transition(.move(edge: .top).combined(with: .opacity))
                }
            }
            .motion(Theme.Motion.standard, value: bridge.lastAdminAction)
            .onChange(of: bridge.lastAdminAction) { _, action in
                guard let action else { return }
                AccessibilityNotification.Announcement(AdminActionBanner.summary(action)).post()
            }
    }
}

struct AdminActionBanner: View {
    let action: AdminAction
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.s3) {
            Image(systemName: "person.badge.shield.checkmark.fill")
                .font(.title3)
                .foregroundStyle(Color.accentColor)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: Theme.Space.s1) {
                Text("Verwaltung")
                    .font(.headline)
                Text(Self.summary(action))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .accessibilityElement(children: .combine)
            Spacer(minLength: 0)
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.footnote.weight(.semibold))
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
            .accessibilityLabel(Text("Hinweis schließen"))
        }
        .padding(.leading, Theme.Space.s4)
        .padding(.vertical, Theme.Space.s2)
        .glassSurface(in: .rect(cornerRadius: Theme.Radius.xl))
    }

    /// "„iPhone“ hat „Altes iPad“ entfernt." in one sentence.
    static func summary(_ action: AdminAction) -> String {
        let actor = action.actor.isEmpty ? String(localized: "Der Server") : "„\(action.actor)“"
        let target = action.target.map { "„\($0)“" } ?? String(localized: "ein Gerät")
        switch action.action {
        case AdminAction.rename: return String(localized: "\(actor) hat \(target) umbenannt.")
        case AdminAction.remove: return String(localized: "\(actor) hat \(target) entfernt.")
        case AdminAction.move: return String(localized: "\(actor) hat \(target) in ein anderes Profil verschoben.")
        case AdminAction.promote: return String(localized: "\(actor) hat \(target) zum Admin gemacht.")
        case AdminAction.demote: return String(localized: "\(actor) hat \(target) die Admin-Rechte entzogen.")
        case AdminAction.enroll: return String(localized: "\(actor) hat Face ID für die Verwaltung eingerichtet.")
        case AdminAction.invite: return String(localized: "\(actor) hat einen Kopplungscode erzeugt.")
        case AdminAction.revokeInvite: return String(localized: "\(actor) hat einen Kopplungscode widerrufen.")
        case AdminAction.approve: return String(localized: "\(actor) hat \(target) im Heimnetz gekoppelt.")
        case AdminAction.deny: return String(localized: "\(actor) hat eine Kopplungsanfrage abgelehnt.")
        default: return String(localized: "\(actor) hat etwas an der Bridge geändert.")
        }
    }
}
