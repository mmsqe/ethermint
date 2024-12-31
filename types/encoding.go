package types

import (
	"cosmossdk.io/core/address"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
)

// EncodingConfig specifies the concrete encoding types to use for a given app.
// This is provided for compatibility between protobuf and amino implementations.
type EncodingConfig struct {
	InterfaceRegistry     types.InterfaceRegistry
	Codec                 codec.Codec
	AddressCodec          address.Codec
	ValidatorAddressCodec address.Codec
	TxConfig              client.TxConfig
	Amino                 *codec.LegacyAmino
}
