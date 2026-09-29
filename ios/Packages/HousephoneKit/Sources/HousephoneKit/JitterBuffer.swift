import Foundation

/// Playout buffer for 20-ms audio frames that arrive over TCP: complete and
/// in order, but in bursts. The player pulls one frame per 20 ms.
///
/// - Playout starts (and restarts after an underrun) once `targetDepth`
///   frames are buffered.
/// - An underrun plays the previous frame once (`concealment`, meant to be
///   attenuated), then silence, and raises the target by one frame.
/// - A buffer that stays above target for `shrinkAfterPulls` pulls lowers
///   the target again and drops one frame to cut latency.
/// - Frames beyond `targetDepth + overflowSlack` are dropped (oldest first),
///   so a long network stall doesn't turn into seconds of delay.
///
/// It counts pulls rather than reading a clock, so tests drive it with
/// simulated time.
public struct JitterBuffer: Sendable {
    public struct Configuration: Sendable, Equatable {
        /// 60 ms.
        public var minimumDepth = 3
        /// 200 ms.
        public var maximumDepth = 10
        public var initialDepth = 3
        public var overflowSlack = 5
        /// 5 s at 20 ms per pull.
        public var shrinkAfterPulls = 250

        public init() {}
    }

    public enum Output: Sendable, Equatable {
        case frame(Data)
        /// Nothing arrived in time; play this (the last frame) attenuated.
        case concealment(Data)
        case silence
    }

    public struct Statistics: Sendable, Equatable {
        public var underruns = 0
        public var droppedFrames = 0
        public var concealedFrames = 0
    }

    public let configuration: Configuration
    public private(set) var targetDepth: Int
    public private(set) var statistics = Statistics()

    private var queue: [Data] = []
    private var isPlaying = false
    private var lastFrame: Data?
    private var missingInARow = 0
    private var pullsAboveTarget = 0

    public init(configuration: Configuration = Configuration()) {
        self.configuration = configuration
        targetDepth = min(max(configuration.initialDepth, configuration.minimumDepth), configuration.maximumDepth)
    }

    public var bufferedFrames: Int { queue.count }

    public mutating func push(_ frame: Data) {
        queue.append(frame)
        let limit = targetDepth + configuration.overflowSlack
        if queue.count > limit {
            let excess = queue.count - limit
            queue.removeFirst(excess)
            statistics.droppedFrames += excess
        }
    }

    /// Called once per 20 ms by the player.
    public mutating func pull() -> Output {
        if !isPlaying {
            guard queue.count >= targetDepth else { return missing(countsAsUnderrun: false) }
            isPlaying = true
        }

        guard !queue.isEmpty else {
            isPlaying = false
            statistics.underruns += 1
            targetDepth = min(targetDepth + 1, configuration.maximumDepth)
            pullsAboveTarget = 0
            return missing(countsAsUnderrun: true)
        }

        let frame = queue.removeFirst()
        lastFrame = frame
        missingInARow = 0

        if queue.count > targetDepth {
            pullsAboveTarget += 1
            if pullsAboveTarget >= configuration.shrinkAfterPulls {
                pullsAboveTarget = 0
                if targetDepth > configuration.minimumDepth { targetDepth -= 1 }
                if queue.count > targetDepth {
                    queue.removeFirst()
                    statistics.droppedFrames += 1
                }
            }
        } else {
            pullsAboveTarget = 0
        }
        return .frame(frame)
    }

    /// Forget everything, e.g. when a call ends or the connection restarts.
    public mutating func reset() {
        queue.removeAll()
        isPlaying = false
        lastFrame = nil
        missingInARow = 0
        pullsAboveTarget = 0
        targetDepth = min(max(configuration.initialDepth, configuration.minimumDepth), configuration.maximumDepth)
    }

    private mutating func missing(countsAsUnderrun: Bool) -> Output {
        missingInARow += 1
        if countsAsUnderrun, missingInARow == 1, let lastFrame {
            statistics.concealedFrames += 1
            return .concealment(lastFrame)
        }
        return .silence
    }
}
