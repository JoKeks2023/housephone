import HousephoneKit
import SwiftUI

/// First screen when no bridge is paired.
struct OnboardingView: View {
    @Environment(AppModel.self) private var appModel
    @State private var showsScanner = false
    @State private var scannedLink: PairingLink?
    @State private var setsUpDirect = false
    @State private var browser = BridgeBrowser()
    @State private var lanTarget: LanPairingTarget?
    @State private var entersAddress = false
    @State private var typedAddress: URL?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.s10) {
                header
                features
            }
            .padding(.horizontal, Theme.Space.s6)
            .padding(.top, Theme.Space.s16)
            .padding(.bottom, Theme.Space.s8)
            .frame(maxWidth: 520, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(alignment: .top) { AmbientGlow() }
        .safeAreaInset(edge: .bottom) { actions }
        .sheet(isPresented: $showsScanner, onDismiss: {
            // Present the pairing sheet only after the scanner is gone;
            // two sheets can't transition at the same time.
            if let scannedLink {
                self.scannedLink = nil
                appModel.pairingLink = scannedLink
            }
        }) {
            QRScannerSheet { link in
                scannedLink = link
                showsScanner = false
            }
        }
        .sheet(isPresented: $setsUpDirect) {
            DirectSetupView()
        }
        .sheet(item: $lanTarget) { target in
            LanPairingView(target: target)
        }
        .sheet(isPresented: $entersAddress, onDismiss: {
            if let typedAddress {
                self.typedAddress = nil
                lanTarget = .address(typedAddress)
            }
        }) {
            LanAddressEntryView { url in typedAddress = url }
        }
        // Browse only while this screen is visible.
        .onAppear { browser.start() }
        .onDisappear { browser.stop() }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s4) {
            AppGlyph()
            Text("Housephone")
                .font(.largeTitle.weight(.semibold))
                .accessibilityAddTraits(.isHeader)
            Text("Dein Festnetz auf dem iPhone – über deine FRITZ!Box, zu Hause und unterwegs.")
                .font(.title3)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var features: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s6) {
            FeatureRow(symbol: "phone.badge.waveform", title: "Klingelt wie ein echter Anruf", detail: "Mit CallKit – auch im Sperrbildschirm und in der Anrufliste.")
            FeatureRow(symbol: "antenna.radiowaves.left.and.right", title: "Überall erreichbar", detail: "Ohne VPN, über deine eigene Bridge.")
            FeatureRow(symbol: "waveform", title: "HD-Sprachqualität", detail: "G.722 von der FRITZ!Box bis ins Ohr.")
        }
    }

    private var actions: some View {
        VStack(spacing: Theme.Space.s3) {
            Text("Mit Bridge")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityAddTraits(.isHeader)
            DiscoveredBridgesView(browser: browser) { found in
                lanTarget = .discovered(found)
            }
            if browser.bridges.isEmpty {
                Button {
                    showsScanner = true
                } label: {
                    Label("QR-Code der Bridge scannen", systemImage: "qrcode.viewfinder")
                        .font(.body.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, Theme.Space.s2)
                }
                .glassButtonStyle(prominent: true)
                .controlSize(.large)
            } else {
                Button {
                    showsScanner = true
                } label: {
                    Label("Mit QR-Code koppeln", systemImage: "qrcode.viewfinder")
                        .font(.body.weight(.medium))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, Theme.Space.s1)
                }
                .glassButtonStyle()
                .controlSize(.large)
            }

            PasteButton(payloadType: String.self) { strings in
                guard let text = strings.first else { return }
                do throws(PairingLinkError) {
                    appModel.pairingLink = try PairingLink(string: text)
                } catch {
                    appModel.pairingLinkError = error
                }
            }
            .buttonBorderShape(.capsule)
            .tint(.secondary)

            Button("Adresse eingeben (Tailscale)") {
                entersAddress = true
            }
            .font(.footnote)
            .tint(.secondary)

            Text("Den QR-Code zeigt dein Server mit `housephone-bridge pair` an – oder kopiere den Kopplungslink und tippe auf „Einsetzen“.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.top, Theme.Space.s1)

            Button {
                setsUpDirect = true
            } label: {
                Label("Direkt mit FRITZ!Box (nur zu Hause)", systemImage: "house")
                    .font(.body.weight(.medium))
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, Theme.Space.s1)
            }
            .glassButtonStyle()
            .controlSize(.large)
            .padding(.top, Theme.Space.s3)
            .accessibilityHint(Text("Ohne Bridge, nur im Heim-WLAN, ohne Apple Watch."))
        }
        .padding(.horizontal, Theme.Space.s6)
        .padding(.bottom, Theme.Space.s4)
        .frame(maxWidth: 520)
    }
}

private struct FeatureRow: View {
    let symbol: String
    let title: LocalizedStringKey
    let detail: LocalizedStringKey

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.s4) {
            Image(systemName: symbol)
                .font(.title2)
                .foregroundStyle(Color.accentColor)
                .frame(width: 32)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.headline)
                Text(detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// House with a handset inside, as on the app icon: accent only, the
/// handset cut out of the house.
struct AppGlyph: View {
    var size: CGFloat = 64

    var body: some View {
        ZStack {
            Image(systemName: "house.fill")
                .font(.system(size: size * 0.62, weight: .semibold))
                .foregroundStyle(Color.accentColor)
            Image(systemName: "phone.fill")
                .font(.system(size: size * 0.2, weight: .bold))
                .foregroundStyle(Color(.systemBackground))
                // Sits in the body of the house, below the roof.
                .offset(y: size * 0.08)
        }
        .frame(width: size, height: size)
        .background(Color.accentColor.opacity(0.14), in: .rect(cornerRadius: Theme.Radius.xl))
        .accessibilityHidden(true)
    }
}

/// Soft accent glow behind hero content (ambient material, low opacity).
struct AmbientGlow: View {
    var body: some View {
        RadialGradient(
            colors: [Color.accentColor.opacity(0.22), .clear],
            center: .top,
            startRadius: 0,
            endRadius: 420
        )
        .frame(height: 520)
        .ignoresSafeArea()
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

/// The bridges Bonjour found in the home network, each one tap away from
/// pairing; while none is found, a quiet search hint.
private struct DiscoveredBridgesView: View {
    let browser: BridgeBrowser
    var onSelect: (DiscoveredBridge) -> Void

    var body: some View {
        if browser.bridges.isEmpty {
            switch browser.state {
            case .searching:
                Label {
                    Text("Suche im Heim-WLAN nach deiner Bridge …")
                } icon: {
                    ProgressView()
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
            case .denied:
                VStack(alignment: .leading, spacing: Theme.Space.s1) {
                    Text("Housephone darf das lokale Netzwerk nicht nutzen und findet deine Bridge deshalb nicht.")
                    Link("In den Einstellungen erlauben", destination: URL(string: UIApplication.openSettingsURLString)!)
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
            case .idle, .failed:
                EmptyView()
            }
        } else {
            ForEach(browser.bridges) { found in
                Button {
                    onSelect(found)
                } label: {
                    HStack(spacing: Theme.Space.s3) {
                        Image(systemName: "house.fill")
                            .font(.title3)
                            .foregroundStyle(Color.accentColor)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Bridge „\(found.name)“ gefunden")
                                .font(.body.weight(.semibold))
                            Text("Im Heim-WLAN – tippen zum Koppeln")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        Spacer(minLength: 0)
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .accessibilityHidden(true)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, Theme.Space.s2)
                }
                .glassButtonStyle(prominent: true)
                .controlSize(.large)
                .accessibilityHint(Text("Koppelt dieses iPhone mit der Bridge, nachdem du einen Code verglichen hast."))
            }
        }
    }
}
