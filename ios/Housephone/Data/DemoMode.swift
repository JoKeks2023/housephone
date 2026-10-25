import Foundation
import HousephoneKit
import Observation
import SwiftData

/// A look around the app without a bridge or FRITZ!Box, for App Review and
/// for anyone who wants to try it first. Nothing leaves the device: calls
/// are simulated inside `CallCenter` (no CallKit, no network, no audio), the
/// recents are sample entries with fictional numbers.
@MainActor
@Observable
final class DemoMode {
    private static let key = "demo.enabled"

    private(set) var isActive = UserDefaults.standard.bool(forKey: DemoMode.key)

    /// Fixed IDs so leaving the demo removes exactly the sample entries.
    private static let sampleIDs: [UUID] = (1...5).map { UUID(uuidString: "00000000-0000-4000-8000-00000000D00\($0)")! }

    func start(in context: ModelContext) {
        seed(in: context)
        isActive = true
        UserDefaults.standard.set(true, forKey: Self.key)
    }

    func stop(in context: ModelContext) {
        removeSamples(from: context)
        isActive = false
        UserDefaults.standard.set(false, forKey: Self.key)
    }

    private func seed(in context: ModelContext) {
        removeSamples(from: context)
        let now = Date()
        // Reserved fictional range (Berlin, 030 5550…), not real subscribers.
        let samples: [(CallDirection, String, String?, TimeInterval, TimeInterval?, CallOutcome)] = [
            (.incoming, "03055500101", "Anna Beispiel", 3_600, 184, .answered),
            (.outgoing, "03055500102", "Werkstatt Muster", 26 * 3_600, 62, .answered),
            (.incoming, "03055500103", "Familie Muster", 30 * 3_600, nil, .missed),
            (.outgoing, "03055500104", nil, 50 * 3_600, nil, .cancelled),
            (.incoming, "03055500105", "Praxis Beispiel", 74 * 3_600, 95, .answered),
        ]
        for (index, sample) in samples.enumerated() {
            context.insert(CallRecord(
                callId: Self.sampleIDs[index], direction: sample.0, number: sample.1, name: sample.2,
                date: now.addingTimeInterval(-sample.3), duration: sample.4, outcome: sample.5
            ))
        }
        try? context.save()
    }

    private func removeSamples(from context: ModelContext) {
        let ids = Self.sampleIDs
        let descriptor = FetchDescriptor<CallRecord>(predicate: #Predicate { ids.contains($0.callId) })
        for record in (try? context.fetch(descriptor)) ?? [] { context.delete(record) }
        try? context.save()
    }
}
