import Foundation
import Testing
@testable import HousephoneKit

/// The direct TR-064 parsers follow the bridge's Go parsers
/// (`bridge/internal/fritzbox`); fictional data only.
struct TR064Tests {
    @Test func readsSOAPResponseValues() {
        let xml = """
        <?xml version="1.0"?>
        <s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
        <s:Body><u:GetPhonebookResponse xmlns:u="urn:dslforum-org:service:X_AVM-DE_OnTel:1">
        <NewPhonebookName>Telefonbuch</NewPhonebookName>
        <NewPhonebookURL>https://192.0.2.1:49443/phonebook.lua?sid=abc&amp;pbid=0</NewPhonebookURL>
        </u:GetPhonebookResponse></s:Body></s:Envelope>
        """
        let values = TR064XML.responseValues(Data(xml.utf8), element: "GetPhonebookResponse")
        #expect(values?["NewPhonebookName"] == "Telefonbuch")
        #expect(values?["NewPhonebookURL"] == "https://192.0.2.1:49443/phonebook.lua?sid=abc&pbid=0")
        #expect(TR064XML.responseValues(Data(xml.utf8), element: "GetCallListResponse") == nil)
        #expect(TR064XML.responseValues(Data("<broken".utf8), element: "x") == nil)
    }

    @Test func readsSOAPFaults() {
        let xml = """
        <s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail>
        <UPnPError xmlns="urn:dslforum-org:control-1-0"><errorCode>606</errorCode><errorDescription>Action Not Authorized</errorDescription></UPnPError>
        </detail></s:Fault></s:Body></s:Envelope>
        """
        #expect(TR064XML.faultCode(Data(xml.utf8)) == "606")
    }

    @Test func escapesEnvelopeArguments() {
        let envelope = TR064XML.envelope(action: "GetPhonebook", service: "urn:x", arguments: [("NewPhonebookID", "<1&2>")])
        #expect(envelope.contains("<u:GetPhonebook xmlns:u=\"urn:x\"><NewPhonebookID>&lt;1&amp;2&gt;</NewPhonebookID></u:GetPhonebook>"))
    }

    @Test func mapsPhonebookLikeTheBridge() throws {
        let xml = """
        <?xml version="1.0" encoding="utf-8"?>
        <phonebooks><phonebook name="Telefonbuch">
        <contact><category>1</category><person><realName>Erika Beispiel</realName></person><uniqueid>7</uniqueid>
          <telephony><number type="home" prio="1">030 / 55 50-100</number><number type="fax_work">0305550199</number><number type="car">0170 5550100</number></telephony></contact>
        <contact><person><realName>Nur Fax</realName></person><telephony><number type="fax_work">0305550198</number></telephony></contact>
        <contact><person><realName></realName></person><telephony><number type="mobile">+49 170 5550101</number></telephony></contact>
        </phonebook></phonebooks>
        """
        let contacts = try TR064XML.phonebook(Data(xml.utf8), id: "0", name: nil)
        #expect(contacts.count == 2)
        #expect(contacts[0] == FritzBoxContact(id: "0-7", name: "Erika Beispiel", favorite: true, phonebook: "Telefonbuch", numbers: [
            FritzBoxNumber(number: "0305550100", type: .home, preferred: true),
            FritzBoxNumber(number: "01705550100", type: .other, preferred: false),
        ]))
        #expect(contacts[1].id == "0-n3")
        #expect(contacts[1].name == "+491705550101")
        #expect(try TR064XML.phonebook(Data(xml.utf8), id: "1", name: "Privat")[0].phonebook == "Privat")
        #expect(throws: TR064Error.self) { try TR064XML.phonebook(Data("<a>".utf8), id: "0", name: nil) }
    }

    @Test func mapsCallListLikeTheBridge() throws {
        let xml = """
        <?xml version="1.0" encoding="utf-8"?>
        <root><timestamp>1</timestamp>
        <Call><Id>10</Id><Type>1</Type><Caller>0305550100</Caller><Called>620</Called><Name>Erika</Name><Device>Housephone</Device><Port>620</Port><Date>28.09.26 18:05</Date><Duration>0:03</Duration></Call>
        <Call><Id>11</Id><Type>2</Type><Caller></Caller><Called>620</Called><Port>620</Port><Date>28.09.26 19:00</Date><Duration>0:00</Duration></Call>
        <Call><Id>12</Id><Type>1</Type><Caller>0305550101</Caller><Port>40</Port><Date>28.09.26 19:00</Date><Duration>1:02</Duration></Call>
        <Call><Id>13</Id><Type>3</Type><Called>*31#0305550102</Called><Port>620</Port><Date>27.09.26 08:00</Date><Duration>0:01</Duration></Call>
        <Call><Id>14</Id><Type>1</Type><Caller>0305550103</Caller><Port>5</Port><Date>27.09.26 08:00</Date><Duration>0:01</Duration></Call>
        <Call><Id>15</Id><Type>6</Type><Caller>0305550104</Caller><Date>27.09.26 08:00</Date></Call>
        <Call><Id>16</Id><Type>1</Type><Caller>0305550105</Caller><Date>kaputt</Date></Call>
        </root>
        """
        let zone = TimeZone(identifier: "Europe/Berlin")!
        let calls = try TR064XML.callList(Data(xml.utf8), timeZone: zone)
        #expect(calls.map(\.id) == ["12", "11", "10", "13"])
        #expect(calls[0].answeredBy == .answeringMachine)
        #expect(calls[0].durationSeconds == 62 * 60)
        #expect(calls[1].isMissed && calls[1].number == "" && calls[1].answeredBy == nil)
        #expect(calls[2].answeredBy == .phone && calls[2].name == "Erika" && calls[2].device == "Housephone")
        #expect(calls[3].direction == .outgoing && calls[3].number == "*31#0305550102")
        var components = DateComponents(year: 2026, month: 9, day: 28, hour: 18, minute: 5)
        components.timeZone = zone
        #expect(calls[2].startedAt == Calendar(identifier: .gregorian).date(from: components))
    }

    @Test func cleansNumbersAndDurations() {
        #expect(TR064XML.cleanNumber(" +49 (30) 555-0100 ") == "+49305550100")
        #expect(TR064XML.cleanNumber("0+1") == "01")
        #expect(TR064XML.cleanNumber("**610#") == "**610#")
        #expect(TR064XML.durationSeconds("0:00") == 0)
        #expect(TR064XML.durationSeconds("2:05") == 125 * 60)
        #expect(TR064XML.durationSeconds("x") == 0)
    }
}
