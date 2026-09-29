# Housephone

Mit iPhone und Apple Watch über die **FRITZ!Box** telefonieren – zu Hause und unterwegs, **ohne VPN**, mit CallKit wie bei normalen Anrufen.

```
FRITZ!Box ◄─SIP/RTP (LAN)─► Bridge (Heimserver) ◄── WSS (Cloudflare Tunnel) ──► iPhone / Apple Watch
                               │  ◄════ Ton: WebRTC über UDP 50000 (iPhone), WebSocket (Watch) ════►
                               └─ VoIP-Push (APNs) ──► Apple ──► iPhone / Apple Watch
```

- **Bridge** ([`bridge/`](bridge/)): Go-Dienst auf dem Heimserver.
  - meldet sich an der FRITZ!Box als IP-Telefon an
  - weckt Geräte per VoIP-Push
  - reicht Gespräche ohne Transcoding durch (G.722 HD auf dem iPhone, G.711 auf der Watch)
- **iPhone-App** ([`ios/`](ios/)): SwiftUI, iOS 26, Liquid Glass.
  - CallKit und PushKit
  - Tastenfeld, Anrufliste, Kontakte
  - Rückruf aus der iPhone-Anrufliste
- **Watch-App** ([`ios/HousephoneWatch`](ios/HousephoneWatch)): eigene CallKit-App für watchOS 26.
  - Die Uhr klingelt selbst.
  - Annehmen und Sprechen am Handgelenk, auch ohne iPhone in der Nähe.

## Loslegen

**[`docs/setup.md`](docs/setup.md)** – von null bis zum ersten Anruf (FRITZ!Box, APNs-Key, Cloudflare Tunnel, Bridge, App, Kopplung, Test-Checkliste).

## Dokumentation

| Thema | Dokument |
|---|---|
| Architektur | [ADR-0001 Bridge](docs/architecture/ADR-0001-bridge-architektur.md), [ADR-0002 Watch](docs/architecture/ADR-0002-watch.md) |
| Protokoll Bridge ↔ Geräte | [`docs/protocol/signaling-v1.md`](docs/protocol/signaling-v1.md) + [Fixtures](docs/protocol/fixtures/) |
| Bridge betreiben | [`bridge/README.md`](bridge/README.md) |
| Apps bauen | [`ios/README.md`](ios/README.md) |
| Aufgaben und Berichte | [`docs/tasks/`](docs/tasks/) |

## Entwicklung

```sh
cd bridge && go test -race ./...                   # Bridge
cd ios/Packages/HousephoneKit && swift test        # Protokoll, Zustände, Audio-Bausteine
cd ios && xcodegen generate && open Housephone.xcodeproj
```

Die CI (`.github/workflows/`) testet die Bridge (inkl. Docker-Image) und baut die iPhone-App samt eingebetteter Watch-App.

---

## Legacy: Telephone für macOS

Dieses Repository ist aus [64characters/Telephone](https://github.com/64characters/Telephone) entstanden. Der macOS-Code (`Telephone/`, `Domain/`, `UseCases/` …) liegt weiterhin hier und dient als Referenz. Die ursprüngliche Anleitung folgt unverändert.

Telephone is a VoIP SIP softphone for Mac. It allows you to make phone
calls over the Internet or your company network. If your phone line
supports SIP protocol, you can use it on your Mac instead of a
physical phone anywhere you have a decent network connection.

### Building (legacy)

#### Opus

Opus codec is optional.

Download:

    $ curl -O https://archive.mozilla.org/pub/opus/opus-1.3.1.tar.gz
    $ tar xzvf opus-1.3.1.tar.gz
    $ cd opus-1.3.1

Build and install:

    $ ./configure --prefix=/path/to/Telephone/ThirdParty/Opus --disable-shared CFLAGS='-arch arm64 -arch x86_64 -Os -mmacosx-version-min=15.6'
    $ make
    $ make install

#### LibreSSL

Download:

    $ curl -O https://ftp.openbsd.org/pub/OpenBSD/LibreSSL/libressl-3.1.5.tar.gz
    $ curl -O https://ftp.openbsd.org/pub/OpenBSD/LibreSSL/libressl-3.1.5.tar.gz.asc
    $ gpg --verify libressl-3.1.5.tar.gz.asc
    $ tar xzvf libressl-3.1.5.tar.gz
    $ cd libressl-3.1.5

Build and install:

    $ ./configure --prefix=/path/to/Telephone/ThirdParty/LibreSSL --disable-shared CFLAGS='-arch arm64 -arch x86_64 -Os -mmacosx-version-min=15.6'
    $ make
    $ make install

#### PJSIP

Download:

    $ curl -o pjproject-2.10.tar.gz https://codeload.github.com/pjsip/pjproject/tar.gz/2.10
    $ tar xzvf pjproject-2.10.tar.gz
    $ cd pjproject-2.10

Create `pjlib/include/pj/config_site.h`:

    #define PJSIP_DONT_SWITCH_TO_TCP 1
    #define PJSUA_MAX_ACC 32
    #define PJMEDIA_RTP_PT_TELEPHONE_EVENTS 101
    #define PJ_DNS_MAX_IP_IN_A_REC 32
    #define PJ_DNS_SRV_MAX_ADDR 32
    #define PJSIP_MAX_RESOLVED_ADDRESSES 32
    #define PJ_HAS_IPV6 1

Patch:

    $ patch -p0 -i /path/to/Telephone/ThirdParty/PJSIP/patches/sock_qos_darwin.patch
    $ patch -p0 -i /path/to/Telephone/ThirdParty/PJSIP/patches/os_core_unix.patch
    $ patch -p0 -i /path/to/Telephone/ThirdParty/PJSIP/patches/coreaudio_dev.patch

Build and install (remove `--with-opus` option if you don’t need Opus):

    $ ./configure --prefix=/path/to/Telephone/ThirdParty/PJSIP --with-opus=/path/to/Telephone/ThirdParty/Opus --with-ssl=/path/to/Telephone/ThirdParty/LibreSSL --disable-video --disable-libyuv --disable-libwebrtc --host=arm-apple-darwin CFLAGS='-arch arm64 -arch x86_64 -Os -DNDEBUG -mmacosx-version-min=15.6' CXXFLAGS='-arch arm64 -arch x86_64 -Os -DNDEBUG -mmacosx-version-min=15.6'
    $ make lib
    $ make install

    
Build Telephone.

### Contribution

For the legal reasons, pull requests are not accepted. Please feel
free to share your thoughts and ideas by commenting on the issues.
