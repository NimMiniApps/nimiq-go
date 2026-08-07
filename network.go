package nimiq

import "fmt"

// NetworkID identifies the chain a transaction is valid on. It is part of the
// signing content, so a transaction signed for one network can never be
// replayed on another.
type NetworkID uint8

// Albatross (proof-of-stake) networks. These are the only values the SDK will
// sign for.
const (
	NetworkTestAlbatross NetworkID = 5
	NetworkDevAlbatross  NetworkID = 6
	NetworkUnitAlbatross NetworkID = 7
	NetworkMainAlbatross NetworkID = 24
)

// Legacy proof-of-work network IDs, listed so IsAlbatross can reject them and
// so a caller porting old code gets a clear error rather than silently
// producing transactions no Albatross node will accept.
//
// Note that proof-of-work mainnet was 42, while Albatross mainnet is 24.
const (
	networkLegacyTest   NetworkID = 1
	networkLegacyDev    NetworkID = 2
	networkLegacyBounty NetworkID = 3
	networkLegacyDummy  NetworkID = 4
	networkLegacyMain   NetworkID = 42
)

// IsAlbatross reports whether the ID names a proof-of-stake network.
//
// This is not cosmetic: Transaction.SigningContent appends sender_data only for
// Albatross networks, mirroring core-rs (primitives/transaction/src/lib.rs).
func (n NetworkID) IsAlbatross() bool {
	switch n {
	case NetworkTestAlbatross, NetworkDevAlbatross, NetworkUnitAlbatross, NetworkMainAlbatross:
		return true
	}
	return false
}

// IsMainnet reports whether the ID names a network carrying real value.
// Useful for policy checks such as refusing test keys on mainnet.
func (n NetworkID) IsMainnet() bool { return n == NetworkMainAlbatross }

func (n NetworkID) String() string {
	switch n {
	case NetworkTestAlbatross:
		return "test-albatross"
	case NetworkDevAlbatross:
		return "dev-albatross"
	case NetworkUnitAlbatross:
		return "unit-albatross"
	case NetworkMainAlbatross:
		return "main-albatross"
	case networkLegacyTest, networkLegacyDev, networkLegacyBounty, networkLegacyDummy, networkLegacyMain:
		return fmt.Sprintf("legacy-pow(%d)", uint8(n))
	}
	return fmt.Sprintf("unknown(%d)", uint8(n))
}
