package fritzbox

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// callListXML is the FRITZ!Box call list (x_contactSCPD, 5.2).
type callListXML struct {
	Calls []struct {
		ID       string `xml:"Id"`
		Type     string `xml:"Type"`
		Caller   string `xml:"Caller"`
		Called   string `xml:"Called"`
		Name     string `xml:"Name"`
		Device   string `xml:"Device"`
		Port     string `xml:"Port"`
		Date     string `xml:"Date"`
		Duration string `xml:"Duration"`
	} `xml:"Call"`
}

// callTypes maps the FRITZ!Box call type (x_contactSCPD, table 70).
var callTypes = map[string][2]string{
	"1":  {protocol.HistoryIncoming, protocol.HistoryAnswered},
	"2":  {protocol.HistoryIncoming, protocol.HistoryMissed},
	"3":  {protocol.HistoryOutgoing, protocol.HistoryAnswered},
	"9":  {protocol.HistoryIncoming, protocol.HistoryActive},
	"10": {protocol.HistoryIncoming, protocol.HistoryRejected},
	"11": {protocol.HistoryOutgoing, protocol.HistoryActive},
}

// callListDate is the FRITZ!Box format "31.07.12 12:03" in local time.
const callListDate = "02.01.06 15:04"

// parseCallList maps the call list to the v1.2 format, newest first.
// Fax calls (port 5) are dropped; port 6 and 40-49 are answering machines.
func parseCallList(data []byte, loc *time.Location) ([]protocol.HistoryCall, error) {
	var doc callListXML
	if err := newDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("call list: %w", err)
	}
	calls := make([]protocol.HistoryCall, 0, len(doc.Calls))
	for _, c := range doc.Calls {
		kind, ok := callTypes[strings.TrimSpace(c.Type)]
		if !ok {
			continue
		}
		port := strings.TrimSpace(c.Port)
		if port == "5" {
			continue
		}
		started, err := time.ParseInLocation(callListDate, strings.TrimSpace(c.Date), loc)
		if err != nil {
			continue
		}
		call := protocol.HistoryCall{
			ID:              strings.TrimSpace(c.ID),
			Direction:       kind[0],
			Result:          kind[1],
			Name:            strings.TrimSpace(c.Name),
			Device:          strings.TrimSpace(c.Device),
			StartedAt:       protocol.Timestamp(started),
			DurationSeconds: durationSeconds(c.Duration),
		}
		// Caller and Called swap roles with the direction; the own side
		// decides which profile sees the call (ADR-0008).
		if call.Direction == protocol.HistoryIncoming {
			call.Number = cleanNumber(c.Caller)
			call.OwnNumber = strings.TrimSpace(c.Called)
		} else {
			call.Number = cleanNumber(c.Called)
			call.OwnNumber = strings.TrimSpace(c.Caller)
		}
		if call.Direction == protocol.HistoryIncoming && call.Result == protocol.HistoryAnswered {
			call.AnsweredBy = protocol.AnsweredByPhone
			if isAnsweringMachinePort(port) {
				call.AnsweredBy = protocol.AnsweredByAnsweringMachine
			}
		}
		calls = append(calls, call)
	}
	sort.SliceStable(calls, func(i, j int) bool {
		if !calls[i].StartedAt.Equal(calls[j].StartedAt) {
			return calls[i].StartedAt.After(calls[j].StartedAt)
		}
		a, _ := strconv.Atoi(calls[i].ID)
		b, _ := strconv.Atoi(calls[j].ID)
		return a > b
	})
	return calls, nil
}

func isAnsweringMachinePort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && (n == 6 || (n >= 40 && n <= 49))
}

// durationSeconds converts "h:mm" (minutes, rounded up by the FRITZ!Box).
func durationSeconds(s string) int {
	hours, minutes, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0
	}
	h, err1 := strconv.Atoi(hours)
	m, err2 := strconv.Atoi(minutes)
	if err1 != nil || err2 != nil || h < 0 || m < 0 {
		return 0
	}
	return (h*60 + m) * 60
}

// History downloads up to max entries of the call list.
func (c *Client) History(ctx context.Context, max int, loc *time.Location) ([]protocol.HistoryCall, error) {
	values, err := c.call(ctx, onTelControl, onTelService, "GetCallList", nil)
	if err != nil {
		return nil, err
	}
	if values["NewCallListURL"] == "" {
		return nil, &Error{Kind: KindUnsupported, Err: errNoCallList}
	}
	data, err := c.download(ctx, values["NewCallListURL"], url.Values{"max": {strconv.Itoa(max)}})
	if err != nil {
		return nil, err
	}
	calls, err := parseCallList(data, loc)
	if err != nil {
		return nil, &Error{Kind: KindProtocol, Err: err}
	}
	if len(calls) > max {
		calls = calls[:max]
	}
	return calls, nil
}
