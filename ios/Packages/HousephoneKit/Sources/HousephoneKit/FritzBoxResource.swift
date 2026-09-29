import Foundation
import Observation
import os

/// Result of fetching a resource that supports `If-None-Match`.
public enum FritzBoxFetchResult<Value: Sendable>: Sendable {
    case updated(Value, etag: String?)
    /// `304`: the cached value is still current.
    case notModified
}

/// Why the last refresh did not bring fresh data.
public enum FritzBoxLoadFailure: Equatable, Sendable {
    /// The bridge answered `fritzbox_unavailable`: TR-064 not set up, or
    /// the FRITZ!Box refused or did not answer. `message` is the bridge's.
    case unavailable(message: String)
    /// The bridge no longer accepts this device.
    case unauthorized
    /// The bridge could not be reached (offline, timeout, unexpected answer).
    case unreachable
}

/// A source other than the bridge (direct TR-064, ADR-0005) could not
/// deliver; `message` is shown as is.
public struct FritzBoxUnavailableError: Error, Sendable {
    public let message: String

    public init(message: String) {
        self.message = message
    }
}

public enum FritzBoxResourceStatus: Equatable, Sendable {
    /// Nothing fetched yet in this app run; a cached value may be shown.
    case idle
    case loading
    /// The last refresh succeeded; `value` is current.
    case current
    /// The last refresh failed; `value` (if any) is from `fetchedAt`.
    case failed(FritzBoxLoadFailure)
}

/// A value fetched from the bridge (phonebook or call list), cached on disk
/// so it is there offline and right after launch — also when the app is
/// launched in the background for a call and needs a caller name.
@MainActor
@Observable
public final class FritzBoxResource<Value: Codable & Sendable & Equatable> {
    public typealias Fetch = @Sendable (_ etag: String?) async throws -> FritzBoxFetchResult<Value>

    public private(set) var value: Value?
    /// When the bridge last confirmed `value` (200 or 304).
    public private(set) var fetchedAt: Date?
    public private(set) var status: FritzBoxResourceStatus = .idle

    /// Called whenever `value` changes, e.g. to rebuild a lookup index.
    @ObservationIgnored public var onValueChange: ((Value?) -> Void)?

    @ObservationIgnored private var etag: String?
    @ObservationIgnored private let cacheURL: URL?
    @ObservationIgnored private let now: @Sendable () -> Date
    @ObservationIgnored private var inFlight: Task<Void, Never>?
    /// Bumped by `clear()` so a refresh that was running then is dropped.
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "fritzbox")

    private struct Envelope: Codable {
        var value: Value
        var etag: String?
        var fetchedAt: Date
    }

    /// `cacheURL`: file for the cached value, or `nil` for memory only.
    public init(cacheURL: URL?, now: @escaping @Sendable () -> Date = Date.init) {
        self.cacheURL = cacheURL
        self.now = now
        if let cacheURL, let data = try? Data(contentsOf: cacheURL),
           let envelope = try? SignalingCoding.makeDecoder().decode(Envelope.self, from: data) {
            value = envelope.value
            etag = envelope.etag
            fetchedAt = envelope.fetchedAt
        }
    }

    public var isLoading: Bool { status == .loading }

    public var failure: FritzBoxLoadFailure? {
        if case .failed(let failure) = status { failure } else { nil }
    }

    /// Fetches a fresh value. Concurrent calls share one request.
    public func refresh(_ fetch: @escaping Fetch) async {
        if let inFlight {
            await inFlight.value
            return
        }
        let task = Task { await performRefresh(fetch) }
        inFlight = task
        await task.value
        inFlight = nil
    }

    /// Forgets the value, e.g. after unpairing.
    public func clear() {
        generation += 1
        inFlight?.cancel()
        value = nil
        etag = nil
        fetchedAt = nil
        status = .idle
        if let cacheURL { try? FileManager.default.removeItem(at: cacheURL) }
        onValueChange?(nil)
    }

    private func performRefresh(_ fetch: @escaping Fetch) async {
        let started = generation
        status = .loading
        do {
            let result = try await fetch(value == nil ? nil : etag)
            guard generation == started else { return }
            switch result {
            case .updated(let fresh, let freshETag):
                let changed = fresh != value
                value = fresh
                etag = freshETag
                fetchedAt = now()
                if changed { onValueChange?(fresh) }
            case .notModified:
                fetchedAt = now()
            }
            status = .current
            persist()
        } catch {
            guard generation == started else { return }
            status = .failed(Self.failure(for: error))
            logger.info("Refresh failed: \(String(describing: error), privacy: .public)")
        }
    }

    private func persist() {
        guard let cacheURL, let value, let fetchedAt else { return }
        do {
            try FileManager.default.createDirectory(at: cacheURL.deletingLastPathComponent(), withIntermediateDirectories: true)
            let data = try SignalingCoding.makeEncoder().encode(Envelope(value: value, etag: etag, fetchedAt: fetchedAt))
            #if os(iOS) || os(watchOS)
            // Readable after the first unlock: an incoming call may launch
            // the app while the device is locked and needs caller names.
            try data.write(to: cacheURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
            #else
            try data.write(to: cacheURL, options: .atomic)
            #endif
        } catch {
            logger.error("Caching failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    nonisolated static func failure(for error: any Error) -> FritzBoxLoadFailure {
        switch error {
        case SignalingClientError.unauthorized:
            .unauthorized
        case SignalingClientError.bridge(let payload) where payload.code == .fritzboxUnavailable:
            .unavailable(message: payload.message)
        case let error as FritzBoxUnavailableError:
            .unavailable(message: error.message)
        default:
            .unreachable
        }
    }
}
