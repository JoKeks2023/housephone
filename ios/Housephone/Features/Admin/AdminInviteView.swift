import CoreImage.CIFilterBuiltins
import HousephoneKit
import SwiftUI
import UIKit

/// Invite a new device: a one-time pairing code as QR code, like
/// `housephone-bridge pair`, with the profile it joins.
struct AdminInviteView: View {
    @Environment(AdminCenter.self) private var admin
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var profileID = BridgeProfile.defaultID
    @State private var invite: AdminInvite?
    @State private var paired: AdminDevice?

    var body: some View {
        NavigationStack {
            Form {
                if let invite {
                    inviteSection(invite)
                } else {
                    Section {
                        TextField("Name (optional)", text: $name)
                        if admin.multiProfile {
                            Picker("Profil", selection: $profileID) {
                                ForEach(admin.profiles) { profile in
                                    Text(profile.name).tag(profile.id)
                                }
                            }
                        }
                    } footer: {
                        Text("Das neue Gerät scannt den QR-Code in Housephone im Heim-WLAN.")
                    }
                    Section {
                        Button("Code erzeugen") {
                            Task {
                                invite = await admin.invite(name: name, profile: admin.profiles.first { $0.id == profileID })
                            }
                        }
                        .disabled(admin.phase == .working)
                    }
                }
                if case .failed(let message) = admin.phase {
                    Text(message)
                        .font(.footnote)
                        .foregroundStyle(Theme.dangerText)
                }
            }
            .navigationTitle("Gerät einladen")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(paired == nil ? LocalizedStringKey("Abbrechen") : LocalizedStringKey("Fertig")) {
                        if let invite, paired == nil {
                            Task { await admin.revoke(invite) }
                        }
                        dismiss()
                    }
                }
            }
            .task(id: invite?.code) {
                guard let invite else { return }
                await waitForPairing(invite)
            }
            .onAppear {
                profileID = admin.profiles.first?.id ?? BridgeProfile.defaultID
            }
        }
    }

    @ViewBuilder
    private func inviteSection(_ invite: AdminInvite) -> some View {
        Section {
            VStack(spacing: Theme.Space.s3) {
                if let paired {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 56))
                        .foregroundStyle(Theme.call)
                        .accessibilityHidden(true)
                    Text("„\(paired.name)“ ist gekoppelt.")
                        .font(.headline)
                } else {
                    if let image = Self.qrCode(invite.link) {
                        Image(uiImage: image)
                            .interpolation(.none)
                            .resizable()
                            .scaledToFit()
                            .frame(maxWidth: 240)
                            .padding(Theme.Space.s3)
                            .background(.white, in: .rect(cornerRadius: Theme.Radius.lg))
                            .accessibilityLabel(Text("QR-Code zum Koppeln"))
                    }
                    Text(invite.grouped)
                        .font(.callout.monospaced())
                        .textSelection(.enabled)
                    Text("Gültig bis \(invite.expiresAt, format: .dateTime.hour().minute()) · \(invite.profileName)")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Space.s3)
        }
    }

    private func waitForPairing(_ invite: AdminInvite) async {
        while !Task.isCancelled, paired == nil, invite.expiresAt > Date() {
            if let state = await admin.inviteState(invite) {
                if state.used {
                    paired = state.device
                    await admin.refresh()
                    return
                }
                if state.expired { return }
            }
            try? await Task.sleep(for: .seconds(3))
        }
    }

    static func qrCode(_ text: String) -> UIImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        filter.correctionLevel = "M"
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)),
              let cgImage = CIContext().createCGImage(output, from: output.extent)
        else { return nil }
        return UIImage(cgImage: cgImage)
    }
}
