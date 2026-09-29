import Foundation
@preconcurrency import Network

/// One RTP flow over UDP: bound to our SDP port, connected to the port the
/// remote SDP names. Received packets are parsed; everything else is
/// dropped.
public final class RTPSession: @unchecked Sendable {
    public let incoming: AsyncStream<RTPPacket>
    private let continuation: AsyncStream<RTPPacket>.Continuation
    private let connection: NWConnection
    private let queue = DispatchQueue(label: "com.jorisconrad.housephone.rtp", qos: .userInteractive)

    public init(localPort: UInt16, remoteHost: String, remotePort: UInt16) {
        (incoming, continuation) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(50))
        let parameters = NWParameters.udp
        parameters.allowLocalEndpointReuse = true
        parameters.prohibitedInterfaceTypes = [.cellular]
        parameters.serviceClass = .interactiveVoice
        parameters.requiredLocalEndpoint = .hostPort(host: .ipv4(.any), port: NWEndpoint.Port(rawValue: localPort) ?? .any)
        connection = NWConnection(host: NWEndpoint.Host(remoteHost), port: NWEndpoint.Port(rawValue: remotePort) ?? .any, using: parameters)
        connection.stateUpdateHandler = { [continuation] state in
            switch state {
            case .failed, .cancelled: continuation.finish()
            default: break
            }
        }
        connection.start(queue: queue)
        receive()
    }

    private func receive() {
        connection.receiveMessage { [weak self] data, _, _, error in
            guard let self else { return }
            if let data, let packet = RTPPacket(parsing: data) { continuation.yield(packet) }
            if error == nil { receive() }
        }
    }

    /// Fire and forget; a lost datagram is a lost 20 ms.
    public func send(_ datagram: Data) {
        connection.send(content: datagram, completion: .idempotent)
    }

    public func close() {
        connection.cancel()
        continuation.finish()
    }

    /// A port from the dynamic range for our side of the SDP; even, as
    /// RTP convention asks (RTCP would be the next odd one).
    public static func randomPort() -> UInt16 {
        UInt16.random(in: 20_000...29_999) & ~1
    }
}
