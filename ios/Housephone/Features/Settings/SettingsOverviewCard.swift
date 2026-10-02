import HousephoneKit
import SwiftUI

/// The top of the settings: how this iPhone telephones, at a glance.
/// Details and fixes stay in the sections below.
struct SettingsOverviewCard: View {
    @Environment(BridgeConnection.self) private var bridge
    @Environment(DirectPhone.self) private var direct
    @Environment(WatchLink.self) private var watch

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s4) {
            HStack(spacing: Theme.Space.s3) {
                Image(systemName: direct.isEnabled ? "house.fill" : "server.rack")
                    .font(.title2.weight(.medium))
                    .foregroundStyle(.white)
                    .frame(width: 52, height: 52)
                    .background(Theme.accent.gradient, in: .rect(cornerRadius: Theme.Radius.lg))
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: Theme.Space.hairline) {
                    Text(title)
                        .font(.title3.weight(.semibold))
                        .lineLimit(2)
                    Text(direct.isEnabled ? "Direkt mit der FRITZ!Box" : "Über deine Bridge")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)

            Grid(alignment: .leading, horizontalSpacing: Theme.Space.s4, verticalSpacing: Theme.Space.s2) {
                if !direct.isEnabled, let profile = bridge.welcome?.profile, profile.isWorthShowing {
                    GridRow {
                        Text("Profil").foregroundStyle(.secondary)
                        Text(profileLine(profile))
                            .lineLimit(1)
                            .monospacedDigit()
                    }
                    .accessibilityElement(children: .combine)
                }
                GridRow {
                    Text("Leitung").foregroundStyle(.secondary)
                    lineStatus
                }
                GridRow {
                    Text("Unterwegs").foregroundStyle(.secondary)
                    if direct.isEnabled {
                        StatusIndicator(tone: .neutral, label: "Nur zu Hause")
                    } else {
                        StatusIndicator(tone: bridge.pushToken == nil ? .warning : .positive, label: bridge.pushToken == nil ? "Noch nicht bereit" : "Erreichbar")
                    }
                }
                GridRow {
                    Text("Apple Watch").foregroundStyle(.secondary)
                    watchStatus
                }
            }
            .font(.subheadline)
        }
        .padding(.vertical, Theme.Space.s2)
    }

    private var title: String {
        if direct.isEnabled {
            return direct.configuration?.registrar ?? "FRITZ!Box"
        }
        return bridge.welcome?.bridgeName ?? bridge.credentials?.bridgeName ?? String(localized: "Bridge")
    }

    /// "Name · Nummer", or just the name without an own number.
    private func profileLine(_ profile: BridgeProfile) -> String {
        guard let number = profile.number else { return profile.name }
        return "\(profile.name) · \(number)"
    }

    @ViewBuilder
    private var lineStatus: some View {
        if direct.isEnabled {
            switch direct.status {
            case .ready: StatusIndicator(tone: .positive, label: "Bereit")
            case .off, .connecting: StatusIndicator(tone: .neutral, label: "Meldet sich an …", isBusy: true)
            case .notAtHome: StatusIndicator(tone: .warning, label: "Nicht im Heim-WLAN")
            case .wrongPassword, .rejected: StatusIndicator(tone: .negative, label: "Anmeldung abgelehnt")
            }
        } else {
            switch bridge.status {
            case .online(sipRegistered: true): StatusIndicator(tone: .positive, label: "Bereit")
            case .online(sipRegistered: false): StatusIndicator(tone: .warning, label: "FRITZ!Box nicht angemeldet")
            case .connecting, .unpaired: StatusIndicator(tone: .neutral, label: "Verbinde …", isBusy: true)
            case .offline: StatusIndicator(tone: .negative, label: "Nicht erreichbar")
            case .rejected: StatusIndicator(tone: .negative, label: "Kopplung ungültig")
            }
        }
    }

    @ViewBuilder
    private var watchStatus: some View {
        if direct.isEnabled {
            StatusIndicator(tone: .neutral, label: "Nur mit Bridge")
        } else if !watch.isWatchPaired {
            StatusIndicator(tone: .neutral, label: "Keine Watch")
        } else if watch.isPairedWithBridge {
            StatusIndicator(tone: .positive, label: "Gekoppelt")
        } else {
            StatusIndicator(tone: .neutral, label: "Nicht gekoppelt")
        }
    }
}
