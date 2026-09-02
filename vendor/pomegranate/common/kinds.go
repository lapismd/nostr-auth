package common

import "fiatjaf.com/nostr"

const (
	// published so the user can find out where his central/operators are later on
	KindSetupAnnouncement nostr.Kind = 16440

	// created at setup time by the user and sent to central/operators
	KindCentralToken         nostr.Kind = 20443
	KindOperatorRegistration nostr.Kind = 20444
	KindCentralRegistration  nostr.Kind = 20445

	// used in the communication between central and operators, signed always only by the central key
	KindConfiguration   nostr.Kind = 26430
	KindGroupCommit     nostr.Kind = 26432
	KindEventToBeSigned nostr.Kind = 26433
	KindECDHRequest     nostr.Kind = 26434
)

var ForbiddenKinds = []nostr.Kind{
	KindSetupAnnouncement,
	KindCentralToken,
	KindCentralRegistration,
	KindOperatorRegistration,
	1776,
	1777,
}
