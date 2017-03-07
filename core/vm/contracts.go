// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"crypto/sha256"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/crypto/ripemd160"
)

// Precompiled contract is the basic interface for native Go contracts. The implementation
// requires a deterministic gas count based on the input size of the Run method of the
// contract.
type PrecompiledContract interface {
	RequiredGas(input []byte) uint64  // RequiredPrice calculates the contract gas use
	Run(input []byte) ([]byte, error) // Run runs the precompiled contract
}

// PrecompiledContracts contains the default set of ethereum contracts
var PrecompiledContracts = map[common.Address]PrecompiledContract{
	common.BytesToAddress([]byte{1}): &ecrecover{},
	common.BytesToAddress([]byte{2}): &sha256hash{},
	common.BytesToAddress([]byte{3}): &ripemd160hash{},
	common.BytesToAddress([]byte{4}): &dataCopy{},
}

// PrecompiledContractsEIP198 contains the default set of ethereum contracts
// for EIP198.
var PrecompiledContractsEIP198 = map[common.Address]PrecompiledContract{
	common.BytesToAddress([]byte{1}): &ecrecover{},
	common.BytesToAddress([]byte{2}): &sha256hash{},
	common.BytesToAddress([]byte{3}): &ripemd160hash{},
	common.BytesToAddress([]byte{4}): &dataCopy{},
	common.BytesToAddress([]byte{5}): &bigModexp{},
}

// RunPrecompile runs and evaluate the output of a precompiled contract defined in contracts.go
func RunPrecompiledContract(p PrecompiledContract, input []byte, contract *Contract) (ret []byte, err error) {
	gas := p.RequiredGas(input)
	if contract.UseGas(gas) {
		return p.Run(input)
	} else {
		return nil, ErrOutOfGas
	}
}

// ECRECOVER implemented as a native contract
type ecrecover struct{}

func (c *ecrecover) RequiredGas(input []byte) uint64 {
	return params.EcrecoverGas
}

func (c *ecrecover) Run(in []byte) ([]byte, error) {
	const ecRecoverInputLength = 128

	in = common.RightPadBytes(in, ecRecoverInputLength)
	// "in" is (hash, v, r, s), each 32 bytes
	// but for ecrecover we want (r, s, v)

	r := new(big.Int).SetBytes(in[64:96])
	s := new(big.Int).SetBytes(in[96:128])
	v := in[63] - 27

	// tighter sig s values in homestead only apply to tx sigs
	if !allZero(in[32:63]) || !crypto.ValidateSignatureValues(v, r, s, false) {
		log.Trace("ECRECOVER error: v, r or s value invalid")
		return nil
	}
	// v needs to be at the end for libsecp256k1
	pubKey, err := crypto.Ecrecover(in[:32], append(in[64:128], v))
	// make sure the public key is a valid one
	if err != nil {
		log.Trace("ECRECOVER failed", "err", err)
		return nil, nil
	}

	// the first byte of pubkey is bitcoin heritage
	return common.LeftPadBytes(crypto.Keccak256(pubKey[1:])[12:], 32), nil
}

// SHA256 implemented as a native contract
type sha256hash struct{}

// RequiredGas returns the gas required to execute the pre-compiled contract.
//
// This method does not require any overflow checking as the input size gas costs
// required for anything significant is so high it's impossible to pay for.
func (c *sha256hash) RequiredGas(input []byte) uint64 {
	return uint64(len(input)+31)/32*params.Sha256WordGas + params.Sha256Gas
}
func (c *sha256hash) Run(in []byte) ([]byte, error) {
	h := sha256.Sum256(in)
	return h[:], nil
}

// RIPMED160 implemented as a native contract
type ripemd160hash struct{}

// RequiredGas returns the gas required to execute the pre-compiled contract.
//
// This method does not require any overflow checking as the input size gas costs
// required for anything significant is so high it's impossible to pay for.
func (c *ripemd160hash) RequiredGas(input []byte) uint64 {
	return uint64(len(input)+31)/32*params.Ripemd160WordGas + params.Ripemd160Gas
}
func (c *ripemd160hash) Run(in []byte) ([]byte, error) {
	ripemd := ripemd160.New()
	ripemd.Write(in)
	return common.LeftPadBytes(ripemd.Sum(nil), 32), nil
}

// data copy implemented as a native contract
type dataCopy struct{}

// RequiredGas returns the gas required to execute the pre-compiled contract.
//
// This method does not require any overflow checking as the input size gas costs
// required for anything significant is so high it's impossible to pay for.
func (c *dataCopy) RequiredGas(input []byte) uint64 {
	return uint64(len(input)+31)/32*params.IdentityWordGas + params.IdentityGas
}
func (c *dataCopy) Run(in []byte) ([]byte, error) {
	return in, nil
}

// bigModexp implements a native big integer exponential modular operation.
type bigModexp struct{}

// RequiredGas returns the gas required to execute the pre-compiled contract.
//
// This method does not require any overflow checking as the input size gas costs
// required for anything significant is so high it's impossible to pay for.
func (c *bigModexp) RequiredGas(input []byte) uint64 {
	// TODO reword required gas to have error reporting and convert arithmetic
	// to uint64.
	if len(input) < 3*32 {
		input = append(input, make([]byte, 3*32-len(input))...)
	}
	var (
		baseLen = new(big.Int).SetBytes(input[:31])
		expLen  = math.BigMax(new(big.Int).SetBytes(input[32:64]), big.NewInt(1))
		modLen  = new(big.Int).SetBytes(input[65:97])
	)
	x := new(big.Int).Set(math.BigMax(baseLen, modLen))
	x.Mul(x, x)
	x.Mul(x, expLen)
	x.Div(x, new(big.Int).SetUint64(params.QuadCoeffDiv))

	return x.Uint64()
}

func (c *bigModexp) Run(input []byte) ([]byte, error) {
	if len(input) < 3*32 {
		input = append(input, make([]byte, 3*32-len(input))...)
	}
	// why 32-byte? These values won't fit anyway
	var (
		baseLen = new(big.Int).SetBytes(input[:32]).Uint64()
		expLen  = new(big.Int).SetBytes(input[32:64]).Uint64()
		modLen  = new(big.Int).SetBytes(input[64:96]).Uint64()
	)

	input = input[96:]
	if uint64(len(input)) < baseLen {
		input = append(input, make([]byte, baseLen-uint64(len(input)))...)
	}
	base := new(big.Int).SetBytes(input[:baseLen])

	input = input[baseLen:]
	if uint64(len(input)) < expLen {
		input = append(input, make([]byte, expLen-uint64(len(input)))...)
	}
	exp := new(big.Int).SetBytes(input[:expLen])

	input = input[expLen:]
	if uint64(len(input)) < modLen {
		input = append(input, make([]byte, modLen-uint64(len(input)))...)
	}
	mod := new(big.Int).SetBytes(input[:modLen])

	return base.Exp(base, exp, mod).Bytes(), nil
}
