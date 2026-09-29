package sipleg

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emiago/diago/media"
	"github.com/emiago/diago/media/sdp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

// codecG722 matches what diago parses from "a=rtpmap:9 G722/8000".
var codecG722 = media.Codec{Name: "G722", PayloadType: 9, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1}

// localCodecs is the codec list diago negotiates with. Offers are narrowed
// per call to a single audio codec (plus telephone-event).
var localCodecs = []media.Codec{codecG722, media.CodecAudioAlaw, media.CodecAudioUlaw, media.CodecTelephoneEvent8000}

func diagoCodec(c codec.Codec) media.Codec {
	switch c {
	case codec.G722:
		return codecG722
	case codec.PCMA:
		return media.CodecAudioAlaw
	default:
		return media.CodecAudioUlaw
	}
}

// offerChoice is the result of reading an INVITE offer.
type offerChoice struct {
	codec codec.Codec
	// audio and telephoneEvent are the codecs exactly as parsed from the
	// offer, so diago's negotiation (struct equality) matches them.
	audio          media.Codec
	telephoneEvent *media.Codec
}

var errNoCommonCodec = errors.New("offer contains none of G722, PCMA, PCMU")

// chooseFromOffer picks the preferred pass-through codec from an SDP offer,
// parsing it the same way diago does.
func chooseFromOffer(body []byte) (offerChoice, error) {
	if len(body) == 0 {
		return offerChoice{}, errors.New("INVITE without SDP offer")
	}
	sd := sdp.SessionDescription{}
	if err := sdp.Unmarshal(body, &sd); err != nil {
		return offerChoice{}, fmt.Errorf("parse offer: %w", err)
	}
	md, err := sd.MediaDescription("audio")
	if err != nil {
		return offerChoice{}, fmt.Errorf("offer has no audio: %w", err)
	}
	parsed := make([]media.Codec, len(md.Formats))
	n, _ := media.CodecsFromSDPRead(md.Formats, sd.Values("a"), parsed)
	parsed = parsed[:n]

	var (
		offered []codec.Codec
		byName  = map[codec.Codec]media.Codec{}
		te      *media.Codec
	)
	for _, pc := range parsed {
		if strings.EqualFold(pc.Name, "telephone-event") {
			if pc.SampleRate == 8000 && te == nil {
				c := pc
				te = &c
			}
			continue
		}
		c, ok := codec.FromName(pc.Name)
		if !ok || pc.SampleRate != codec.ClockRate {
			continue
		}
		if _, seen := byName[c]; !seen {
			byName[c] = pc
			offered = append(offered, c)
		}
	}
	chosen, ok := codec.ChooseFirst(offered)
	if !ok {
		return offerChoice{}, errNoCommonCodec
	}
	return offerChoice{codec: chosen, audio: byName[chosen], telephoneEvent: te}, nil
}

func (o offerChoice) diagoCodecs() []media.Codec {
	out := []media.Codec{o.audio}
	if o.telephoneEvent != nil {
		out = append(out, *o.telephoneEvent)
	}
	return out
}

// offerSDP renders a minimal SDP that narrows diago's outgoing offer to one
// audio codec plus telephone-event (used as the INVITE "originator").
func offerSDP(c codec.Codec) []byte {
	dc := diagoCodec(c)
	te := media.CodecTelephoneEvent8000
	ip := net.IPv4(127, 0, 0, 1).String()
	lines := []string{
		"v=0",
		"o=- 1 1 IN IP4 " + ip,
		"s=housephone",
		"c=IN IP4 " + ip,
		"t=0 0",
		fmt.Sprintf("m=audio 9 RTP/AVP %d %d", dc.PayloadType, te.PayloadType),
		fmt.Sprintf("a=rtpmap:%d %s/%d", dc.PayloadType, dc.Name, dc.SampleRate),
		fmt.Sprintf("a=rtpmap:%d telephone-event/8000", te.PayloadType),
		fmt.Sprintf("a=fmtp:%d 0-16", te.PayloadType),
		"a=sendrecv",
	}
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}
