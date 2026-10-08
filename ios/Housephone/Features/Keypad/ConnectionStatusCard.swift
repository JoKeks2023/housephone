import HousephoneKit
import SwiftData
import SwiftUI

/// One glass line above the keypad: is the line ready, and through what
/// (bridge or FRITZ!Box directly), plus new missed calls. Quiet when all
/// is well; when the user has to act, it leads to where they can.
struct ConnectionStatusCard: View {
    @Environment(BridgeConnection.self) private var bridge
    @Environment(DirectPhone.self) private var direct
    @Environment(AppModel.self) private var appModel

    @Query(
        filter: #Predicate<CallRecord> { $0.directionRaw == "incoming" && $0.outcomeRaw == "missed" },
        sort: \CallRecord.date,
        order: .reverse
    )
    private var missedCalls: [CallRecord]

    private var newMissed: Int {
        missedCalls.prefix { $0.date > appModel.recentsSeenAt }.count
    }

    private struct Status: Equatable {
        let tone: StatusIndicator.Tone
        let label: LocalizedStringKey
        var isBusy = false
        /// The fix is in the settings.
        var opensSettings = false
    }

    private var status: Status {
        if direct.isEnabled {
            switch direct.status {
            case .ready: return Status(tone: .positive, label: "Bereit · direkt über die FRITZ!Box")
            case .off, .connecting: return Status(tone: .neutral, label: "Melde an der FRITZ!Box an …", isBusy: true)
            case .notAtHome: return Status(tone: .warning, label: "Nur im Heim-WLAN verfügbar")
            case .wrongPassword, .rejected: return Status(tone: .negative, label: "FRITZ!Box lehnt die Anmeldung ab", opensSettings: true)
            }
        }
        switch bridge.status {
        case .online(sipRegistered: true): return Status(tone: .positive, label: "Bereit · über deine Bridge")
        case .online(sipRegistered: false): return Status(tone: .warning, label: "Bridge nicht an der FRITZ!Box angemeldet")
        case .connecting, .unpaired: return Status(tone: .neutral, label: "Verbinde mit der Bridge …", isBusy: true)
        case .offline: return Status(tone: .negative, label: "Bridge nicht erreichbar")
        case .rejected: return Status(tone: .negative, label: "Kopplung ungültig – neu koppeln", opensSettings: true)
        }
    }

    var body: some View {
        let status = status
        GlassGroup(spacing: Theme.Space.s2) {
            HStack(spacing: Theme.Space.s2) {
                statusPill(status)
                if newMissed > 0 {
                    missedPill
                        .transition(.opacity.combined(with: .scale(scale: 0.96)))
                }
            }
        }
        .motion(Theme.Motion.standard, value: status)
        .motion(Theme.Motion.standard, value: newMissed)
    }

    @ViewBuilder
    private func statusPill(_ status: Status) -> some View {
        let content = HStack(spacing: Theme.Space.s2) {
            StatusIndicator(tone: status.tone, label: status.label, isBusy: status.isBusy)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
            if status.opensSettings {
                Image(systemName: "chevron.forward")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
        .padding(.vertical, Theme.Space.s2)
        .padding(.horizontal, Theme.Space.s3)
        .frame(minHeight: 36)
        .glassSurface(in: .capsule, interactive: status.opensSettings)

        if status.opensSettings {
            Button {
                appModel.selectedTab = .settings
            } label: {
                content
            }
            .buttonStyle(PressableButtonStyle())
            .accessibilityHint(Text("Öffnet die Einstellungen"))
        } else {
            content
        }
    }

    private var missedPill: some View {
        Button {
            appModel.recentsShowsMissedOnly = true
            appModel.selectedTab = .recents
        } label: {
            HStack(spacing: Theme.Space.s1) {
                Image(systemName: "phone.arrow.down.left.fill")
                    .imageScale(.small)
                    .accessibilityHidden(true)
                Text("\(newMissed) verpasst")
                    .monospacedDigit()
            }
            .font(.subheadline.weight(.medium))
            .foregroundStyle(Theme.dangerText)
            .padding(.vertical, Theme.Space.s2)
            .padding(.horizontal, Theme.Space.s3)
            .frame(minHeight: 36)
            .glassSurface(in: .capsule, interactive: true)
        }
        .buttonStyle(PressableButtonStyle())
        .accessibilityHint(Text("Zeigt die verpassten Anrufe"))
    }
}
