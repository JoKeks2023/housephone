import AVFAudio
import HousephoneKit

/// Plays the German ringback tone while the remote side rings without
/// sending early media. Only plays while CallKit's audio session is active.
@MainActor
final class RingbackPlayer {
    private var player: AVAudioPlayer?
    private var wantsPlayback = false
    private var isAudioSessionActive = false
    private lazy var tone = RingbackTone.germanWAV()

    func start() {
        wantsPlayback = true
        playIfPossible()
    }

    func stop() {
        wantsPlayback = false
        player?.stop()
        player = nil
    }

    func audioSessionDidActivate() {
        isAudioSessionActive = true
        playIfPossible()
    }

    func audioSessionDidDeactivate() {
        isAudioSessionActive = false
        player?.stop()
        player = nil
    }

    private func playIfPossible() {
        guard wantsPlayback, isAudioSessionActive, player == nil else { return }
        guard let player = try? AVAudioPlayer(data: tone) else { return }
        player.numberOfLoops = -1
        player.volume = 0.5
        player.play()
        self.player = player
    }
}
