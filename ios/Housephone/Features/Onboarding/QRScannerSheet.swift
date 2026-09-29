import HousephoneKit
import SwiftUI
import VisionKit

/// Scans the pairing QR code the bridge prints.
struct QRScannerSheet: View {
    let onLink: (PairingLink) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var rejectedCode = false
    @State private var foundTrigger = 0

    private var scannerAvailable: Bool {
        DataScannerViewController.isSupported && DataScannerViewController.isAvailable
    }

    var body: some View {
        NavigationStack {
            Group {
                if scannerAvailable {
                    QRScanner { payload in
                        handle(payload)
                    }
                    .ignoresSafeArea()
                    .overlay(alignment: .bottom) { hint }
                } else {
                    EmptyStateView(
                        symbol: "camera.fill",
                        title: "Kamera nicht verfügbar",
                        message: "Erlaube den Kamerazugriff in den Einstellungen – oder kopiere den Kopplungslink und füge ihn ein."
                    )
                }
            }
            .navigationTitle("QR-Code scannen")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Abbrechen", role: .cancel) { dismiss() }
                }
            }
        }
        // The camera needs the full height; no half-height detent.
        .presentationDetents([.large])
        .sensoryFeedback(.success, trigger: foundTrigger)
    }

    private var hint: some View {
        Text(rejectedCode ? "Das ist kein Housephone-Code." : "Richte die Kamera auf den QR-Code deiner Bridge.")
            .font(.callout.weight(.medium))
            .padding(.horizontal, Theme.Space.s4)
            .padding(.vertical, Theme.Space.s3)
            .glassEffect()
            .padding(.bottom, Theme.Space.s8)
            .motion(Theme.Motion.standard, value: rejectedCode)
    }

    private func handle(_ payload: String) {
        guard let link = try? PairingLink(string: payload) else {
            rejectedCode = true
            return
        }
        foundTrigger += 1
        onLink(link)
    }
}

private struct QRScanner: UIViewControllerRepresentable {
    let onPayload: (String) -> Void

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let scanner = DataScannerViewController(
            recognizedDataTypes: [.barcode(symbologies: [.qr])],
            qualityLevel: .balanced,
            recognizesMultipleItems: false,
            isHighFrameRateTrackingEnabled: false,
            isPinchToZoomEnabled: true,
            isGuidanceEnabled: true,
            isHighlightingEnabled: true
        )
        scanner.delegate = context.coordinator
        Task { @MainActor in
            try? scanner.startScanning()
        }
        return scanner
    }

    func updateUIViewController(_ scanner: DataScannerViewController, context: Context) {
        context.coordinator.onPayload = onPayload
    }

    static func dismantleUIViewController(_ scanner: DataScannerViewController, coordinator: Coordinator) {
        scanner.stopScanning()
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(onPayload: onPayload)
    }

    @MainActor
    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        var onPayload: (String) -> Void
        private var lastPayload: String?

        init(onPayload: @escaping (String) -> Void) {
            self.onPayload = onPayload
        }

        func dataScanner(_ dataScanner: DataScannerViewController, didAdd addedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            for item in addedItems {
                guard case .barcode(let barcode) = item, let payload = barcode.payloadStringValue, payload != lastPayload else { continue }
                lastPayload = payload
                onPayload(payload)
                return
            }
        }
    }
}
