# T-0016 Rückfall auf WebSocket-Audio

Unterwegs ohne Portfreigabe (UDP 50000) bleibt ein ausgehender Anruf auf dem iPhone bei „Verbinden …“ und scheitert: WebRTC findet keinen Weg zwischen Mobilfunk und Heimnetz. Vorgabe (2026-10-02): einen Rückfallweg bauen, der den Ton durch den Tunnel schickt; die Portfreigabe bleibt der bessere Weg.
