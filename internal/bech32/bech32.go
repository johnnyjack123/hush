// Package bech32 implements the BIP 173 Bech32 encoding without its 90 character limit.
//
// age uses the same unlimited variant for its keys; hush public keys carry two
// 32-byte keys and would not fit into the BIP 173 limit.
// Specification: https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki
package bech32

import (
	"errors"
	"fmt"
	"strings"
)

const charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

var generator = [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

func polymod(values []byte) uint32 {
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 == 1 {
				chk ^= generator[i]
			}
		}
	}
	return chk
}

func hrpExpand(hrp string) []byte {
	out := make([]byte, 0, len(hrp)*2+1)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

func createChecksum(hrp string, data []byte) []byte {
	values := append(hrpExpand(hrp), data...)
	values = append(values, 0, 0, 0, 0, 0, 0)
	mod := polymod(values) ^ 1
	out := make([]byte, 6)
	for i := range out {
		out[i] = byte(mod>>uint(5*(5-i))) & 31
	}
	return out
}

func convertBits(data []byte, fromBits, toBits uint, pad bool) ([]byte, error) {
	var (
		acc  uint32
		bits uint
		out  []byte
	)
	maxv := byte(1<<toBits - 1)
	for _, v := range data {
		if v>>fromBits != 0 {
			return nil, fmt.Errorf("invalid data byte %d", v)
		}
		acc = acc<<fromBits | uint32(v)
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			out = append(out, byte(acc>>bits)&maxv)
		}
	}
	if pad {
		if bits > 0 {
			out = append(out, byte(acc<<(toBits-bits))&maxv)
		}
	} else if bits >= fromBits {
		return nil, errors.New("illegal zero padding")
	} else if byte(acc<<(toBits-bits))&maxv != 0 {
		return nil, errors.New("non-zero padding")
	}
	return out, nil
}

// IsDataChar reports whether c can appear in the data part of a lower-case Bech32 string.
func IsDataChar(c byte) bool {
	return strings.IndexByte(charset, c) >= 0
}

func validHRP(hrp string) error {
	if hrp == "" {
		return errors.New("empty human-readable part")
	}
	for i := 0; i < len(hrp); i++ {
		if hrp[i] < 33 || hrp[i] > 126 {
			return fmt.Errorf("invalid character in human-readable part at %d", i)
		}
	}
	return nil
}

// Encode returns the Bech32 encoding of data with the given human-readable part.
// The output is upper case if hrp is upper case, lower case otherwise.
func Encode(hrp string, data []byte) (string, error) {
	if err := validHRP(hrp); err != nil {
		return "", err
	}
	upper := strings.ToUpper(hrp) == hrp && strings.ToLower(hrp) != hrp
	if !upper && strings.ToLower(hrp) != hrp {
		return "", errors.New("mixed case human-readable part")
	}
	hrp = strings.ToLower(hrp)
	values, err := convertBits(data, 8, 5, true)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.Grow(len(hrp) + 1 + len(values) + 6)
	b.WriteString(hrp)
	b.WriteByte('1')
	for _, v := range values {
		b.WriteByte(charset[v])
	}
	for _, v := range createChecksum(hrp, values) {
		b.WriteByte(charset[v])
	}
	if upper {
		return strings.ToUpper(b.String()), nil
	}
	return b.String(), nil
}

// Decode parses a Bech32 string and returns its lower-cased human-readable part and data.
func Decode(s string) (hrp string, data []byte, err error) {
	if strings.ToLower(s) != s && strings.ToUpper(s) != s {
		return "", nil, errors.New("mixed case")
	}
	s = strings.ToLower(s)
	pos := strings.LastIndexByte(s, '1')
	if pos < 1 || pos+7 > len(s) {
		return "", nil, errors.New("separator at invalid position")
	}
	hrp = s[:pos]
	if err := validHRP(hrp); err != nil {
		return "", nil, err
	}
	values := make([]byte, 0, len(s)-pos-1)
	for i := pos + 1; i < len(s); i++ {
		d := strings.IndexByte(charset, s[i])
		if d < 0 {
			return "", nil, fmt.Errorf("invalid character at %d", i)
		}
		values = append(values, byte(d))
	}
	if polymod(append(hrpExpand(hrp), values...)) != 1 {
		return "", nil, errors.New("invalid checksum")
	}
	data, err = convertBits(values[:len(values)-6], 5, 8, false)
	if err != nil {
		return "", nil, err
	}
	return hrp, data, nil
}
