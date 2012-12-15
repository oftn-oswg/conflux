/*
   conflux - Distributed database synchronization library
	Based on the algorithm described in
		"Set Reconciliation with Nearly Optimal	Communication Complexity",
			Yaron Minsky, Ari Trachtenberg, and Richard Zippel, 2004.

   Copyright (C) 2012  Casey Marshall <casey.marshall@gmail.com>

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package conflux

import (
	"fmt"
	"math/big"
)

var p_128 = big.NewInt(0).SetBytes([]byte{
        0x1,0x11,0xd,0xb2,0x97,0xcd,0x30,0x8d,
        0x90,0xe5,0x3f,0xb8,0xa1,0x30,0x90,0x97,0xe9})

var p_160 = big.NewInt(0).SetBytes([]byte{
        0x1,0xfe,0x90,0xe7,0xb4,0x19,0x88,0xa6,
        0x41,0xb1,0xa6,0xfe,0xc8,0x7d,0x89,0xa3,
        0x1e,0x2a,0x61,0x31,0xf5})

var p_256 = big.NewInt(0).SetBytes([]byte{
        0x1,0xdd,0xf4,0x8a,0xc3,0x45,0x19,0x18,
        0x13,0xab,0x7d,0x92,0x27,0x99,0xe8,0x93,
        0x96,0x19,0x43,0x8,0xa4,0xa5,0x9,0xb,
        0x36,0xc9,0x62,0xd5,0xd5,0xd6,0xdd,0x80,0x27})

var p_512 = big.NewInt(0).SetBytes([]byte{
        0x1,0xc7,0x19,0x72,0x25,0xf4,0xa5,0xd5,
        0x8a,0xc0,0x2,0xa4,0xdc,0x8d,0xb1,0xd9,
        0xb0,0xa1,0x5b,0x7a,0x43,0x22,0x5d,0x5b,
        0x51,0xa8,0x1c,0x76,0x17,0x44,0x2a,0x4a,
        0x9c,0x62,0xdc,0x9e,0x25,0xd6,0xe3,0x12,
        0x1a,0xea,0xef,0xac,0xd9,0xfd,0x8d,0x6c,
        0xb7,0x26,0x6d,0x19,0x15,0x53,0xd7,0xd,
        0xb6,0x68,0x3b,0x65,0x40,0x89,0x18,0x3e,0xbd})

// Zp represents a value in the finite field Z(p),
// an integer in which all arithmetic is (mod p).
type Zp struct {
	*big.Int
	// The prime bound of the finite field Z(p).
	P *big.Int
}

// NewZp creates an integer n in the finite field p.
func NewZp(p int64, n int64) *Zp {
	zp := &Zp{ Int: big.NewInt(n), P: big.NewInt(p) }
	zp.Norm()
	return zp
}

// Normalize the integer to its finite field, (mod P)
func (zp *Zp) Norm() {
	zp.Mod(zp.Int, zp.P)
}

func (zp *Zp) Cmp(x *Zp) int {
	zp.assertP(x)
	return zp.Int.Cmp(x.Int)
}

func (zp *Zp) Add(x, y *Zp) *Zp {
	zp.assertP(x, y)
	zp.Int.Add(x.Int, y.Int)
	zp.Norm()
	return zp
}

func (zp *Zp) Mul(x, y *Zp) *Zp {
	zp.assertP(x, y)
	zp.Int.Mul(x.Int, y.Int)
	zp.Norm()
	return zp
}

func (zp *Zp) assertP(values... *Zp) {
	for _, v := range values {
		if zp.P.Cmp(v.P) != 0 {
			panic(fmt.Sprintf("finite field mismatch betwee Z(%v) and Z(%v)", zp.P, v.P))
		}
	}
}
