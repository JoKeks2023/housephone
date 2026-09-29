import HousephoneKit
import SwiftUI

extension View {
    /// Shows `device.paired` from the bridge until dismissed: every paired
    /// device learns about a new one, so a pairing nobody expected is seen.
    func devicePairedBanner() -> some View {
        modifier(DevicePairedBannerModifier())
    }
}

private struct DevicePairedBannerModifier: ViewModifier {
    @Environment(BridgeConnection.self) private var bridge

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .top) {
                if let paired = bridge.newlyPairedDevice {
                    DevicePairedBanner(paired: paired) {
                        bridge.dismissNewlyPairedDevice()
                    }
                    .padding(.horizontal, Theme.Space.s4)
                    .transition(.move(edge: .top).combined(with: .opacity))
                }
            }
            .motion(Theme.Motion.standard, value: bridge.newlyPairedDevice)
            .onChange(of: bridge.newlyPairedDevice) { _, paired in
                guard let paired else { return }
                AccessibilityNotification.Announcement(
                    String(localized: "Neues Gerät gekoppelt: \(paired.deviceName)")
                ).post()
            }
    }
}

struct DevicePairedBanner: View {
    let paired: DevicePaired
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.s3) {
            Image(systemName: "lock.shield.fill")
                .font(.title3)
                .foregroundStyle(Color.accentColor)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: Theme.Space.s1) {
                Text("Neues Gerät gekoppelt")
                    .font(.headline)
                Text("„\(paired.deviceName)“ wurde \(paired.pairedAt, format: .relative(presentation: .named)) mit deiner Bridge gekoppelt. Warst du das nicht? Entferne das Gerät auf dem Server.")
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
        .glassEffect(.regular, in: .rect(cornerRadius: Theme.Radius.xl))
    }
}
