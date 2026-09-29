package fritzbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// phonebookXML is the FRITZ!Box phonebook export (x_contactSCPD, 5.1).
type phonebookXML struct {
	Phonebooks []struct {
		Name     string       `xml:"name,attr"`
		Contacts []contactXML `xml:"contact"`
	} `xml:"phonebook"`
}

type contactXML struct {
	Category string `xml:"category"`
	RealName string `xml:"person>realName"`
	UniqueID string `xml:"uniqueid"`
	Numbers  []struct {
		Type  string `xml:"type,attr"`
		Prio  string `xml:"prio,attr"`
		Value string `xml:",chardata"`
	} `xml:"telephony>number"`
}

var knownNumberTypes = map[string]bool{
	protocol.NumberTypeHome:   true,
	protocol.NumberTypeMobile: true,
	protocol.NumberTypeWork:   true,
	protocol.NumberTypeIntern: true,
	protocol.NumberTypeMemo:   true,
	protocol.NumberTypeOther:  true,
}

// parsePhonebook maps one downloaded phonebook to contacts (spec v1.2):
// no contacts without numbers, no fax numbers, unknown types -> "other",
// id "<phonebook id>-<uniqueid>". name overrides the name attribute when
// the FRITZ!Box returned one via GetPhonebook.
func parsePhonebook(data []byte, phonebookID, name string) ([]protocol.Contact, error) {
	var doc phonebookXML
	if err := newDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("phonebook %s: %w", phonebookID, err)
	}
	var contacts []protocol.Contact
	index := 0
	for _, book := range doc.Phonebooks {
		bookName := strings.TrimSpace(name)
		if bookName == "" {
			bookName = strings.TrimSpace(book.Name)
		}
		for _, c := range book.Contacts {
			index++
			var numbers []protocol.ContactNumber
			for _, n := range c.Numbers {
				typ := strings.ToLower(strings.TrimSpace(n.Type))
				if typ == protocol.NumberTypeFaxWork {
					continue
				}
				if !knownNumberTypes[typ] {
					typ = protocol.NumberTypeOther
				}
				number := cleanNumber(n.Value)
				if number == "" {
					continue
				}
				numbers = append(numbers, protocol.ContactNumber{
					Number:    number,
					Type:      typ,
					Preferred: strings.TrimSpace(n.Prio) == "1",
				})
			}
			if len(numbers) == 0 {
				continue
			}
			id := strings.TrimSpace(c.UniqueID)
			if id == "" {
				id = "n" + strconv.Itoa(index)
			}
			contactName := strings.TrimSpace(c.RealName)
			if contactName == "" {
				contactName = numbers[0].Number
			}
			contacts = append(contacts, protocol.Contact{
				ID:        phonebookID + "-" + id,
				Name:      contactName,
				Favorite:  strings.TrimSpace(c.Category) == "1",
				Phonebook: bookName,
				Numbers:   numbers,
			})
		}
	}
	return contacts, nil
}

// sortContacts orders by name (case-insensitive), then id. The apps sort
// again with the user's locale.
func sortContacts(contacts []protocol.Contact) {
	sort.SliceStable(contacts, func(i, j int) bool {
		a, b := strings.ToLower(contacts[i].Name), strings.ToLower(contacts[j].Name)
		if a != b {
			return a < b
		}
		return contacts[i].ID < contacts[j].ID
	})
}

// Phonebook downloads all phonebooks of the FRITZ!Box and merges them.
func (c *Client) Phonebook(ctx context.Context) ([]protocol.Contact, error) {
	list, err := c.call(ctx, onTelControl, onTelService, "GetPhonebookList", nil)
	if err != nil {
		return nil, err
	}
	var all []protocol.Contact
	for _, id := range strings.Split(list["NewPhonebookList"], ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		book, err := c.call(ctx, onTelControl, onTelService, "GetPhonebook", [][2]string{{"NewPhonebookID", id}})
		if err != nil {
			return nil, err
		}
		if book["NewPhonebookURL"] == "" {
			continue
		}
		data, err := c.download(ctx, book["NewPhonebookURL"], nil)
		if err != nil {
			return nil, err
		}
		contacts, err := parsePhonebook(data, id, book["NewPhonebookName"])
		if err != nil {
			return nil, &Error{Kind: KindProtocol, Err: err}
		}
		all = append(all, contacts...)
	}
	sortContacts(all)
	if all == nil {
		all = []protocol.Contact{}
	}
	return all, nil
}

// errNoCallList is returned when the call list feature is switched off.
var errNoCallList = errors.New("GetCallList returned no URL (call list disabled)")
