package provider

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"net/url"
	"strconv"
)

func fanqiePad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte(nil), b...)
	for range n {
		out = append(out, byte(n))
	}
	return out
}
func fanqieCBC(b, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := fanqiePad(b)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, out)
	return out, nil
}
func fanqiePBInt(b []byte, field int, v uint64) []byte {
	b = binary.AppendUvarint(b, uint64(field<<3))
	return binary.AppendUvarint(b, v)
}
func fanqiePBBytes(b []byte, field int, v []byte) []byte {
	b = binary.AppendUvarint(b, uint64(field<<3|2))
	b = binary.AppendUvarint(b, uint64(len(v)))
	return append(b, v...)
}

func fanqieSM3(input []byte) []byte {
	h := [8]uint32{0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600, 0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e}
	b := append([]byte(nil), input...)
	length := uint64(len(b)) * 8
	b = append(b, 0x80)
	for len(b)%64 != 56 {
		b = append(b, 0)
	}
	b = binary.BigEndian.AppendUint64(b, length)
	p1 := func(x uint32) uint32 { return x ^ bits.RotateLeft32(x, 15) ^ bits.RotateLeft32(x, 23) }
	for at := 0; at < len(b); at += 64 {
		var w [68]uint32
		for i := 0; i < 16; i++ {
			w[i] = binary.BigEndian.Uint32(b[at+i*4:])
		}
		for i := 16; i < 68; i++ {
			w[i] = p1(w[i-16]^w[i-9]^bits.RotateLeft32(w[i-3], 15)) ^ bits.RotateLeft32(w[i-13], 7) ^ w[i-6]
		}
		a, c0, c, d, e, f, g, h0 := h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7]
		for j := 0; j < 64; j++ {
			t := uint32(0x79cc4519)
			ff, gg := a^c0^c, e^f^g
			if j >= 16 {
				t = 0x7a879d8a
				ff = (a & c0) | (a & c) | (c0 & c)
				gg = (e & f) | (^e & g)
			}
			ss1 := bits.RotateLeft32(bits.RotateLeft32(a, 12)+e+bits.RotateLeft32(t, j), 7)
			ss2 := ss1 ^ bits.RotateLeft32(a, 12)
			tt1 := ff + d + ss2 + (w[j] ^ w[j+4])
			tt2 := gg + h0 + ss1 + w[j]
			d = c
			c = bits.RotateLeft32(c0, 9)
			c0 = a
			a = tt1
			h0 = g
			g = bits.RotateLeft32(f, 19)
			f = e
			e = tt2 ^ bits.RotateLeft32(tt2, 9) ^ bits.RotateLeft32(tt2, 17)
		}
		for i, v := range []uint32{a, c0, c, d, e, f, g, h0} {
			h[i] ^= v
		}
	}
	out := make([]byte, 0, 32)
	for _, v := range h {
		out = binary.BigEndian.AppendUint32(out, v)
	}
	return out
}

func fanqieClientSign(query string, ts int64) (map[string]string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return nil, err
	}
	aid, err := strconv.ParseUint(q.Get("aid"), 10, 64)
	if err != nil {
		return nil, err
	}
	const licenseID = 1611921764
	var pb []byte
	pb = fanqiePBInt(pb, 1, 0x20200929<<1)
	pb = fanqiePBInt(pb, 2, 2)
	pb = fanqiePBInt(pb, 3, uint64(binary.LittleEndian.Uint32(random[:4])&0x7fffffff))
	for _, field := range []struct {
		n int
		v string
	}{{4, q.Get("aid")}, {5, q.Get("device_id")}, {6, "1611921764"}, {7, q.Get("version_name")}, {8, "v04.04.05-ov-android"}} {
		pb = fanqiePBBytes(pb, field.n, []byte(field.v))
	}
	pb = fanqiePBInt(pb, 9, 134744640)
	pb = fanqiePBBytes(pb, 10, make([]byte, 8))
	pb = fanqiePBInt(pb, 11, 0)
	pb = fanqiePBInt(pb, 12, uint64(ts)<<1)
	pb = fanqiePBBytes(pb, 13, fanqieSM3(make([]byte, 16))[:6])
	pb = fanqiePBBytes(pb, 14, fanqieSM3([]byte(query))[:6])
	pb = fanqiePBInt(pb, 20, 738)
	var environment []byte
	environment = fanqiePBBytes(environment, 1, []byte(q.Get("device_type")))
	environment = fanqiePBBytes(environment, 2, []byte(q.Get("os_version")))
	environment = fanqiePBBytes(environment, 3, []byte("googleplay"))
	environment = fanqiePBInt(environment, 4, 20480)
	pb = fanqiePBBytes(pb, 23, environment)
	signKey, _ := hex.DecodeString("ac1adaae95a7af94a5114ab3b3a97dd80050aa0a39314c40528caec95256c28c")
	scheduleBytes := fanqieSM3(append(append(append([]byte(nil), signKey...), 0xf2, 0x81, 0x61, 0x6f), signKey...))
	var k [72]uint64
	for i := 0; i < 4; i++ {
		k[i] = binary.LittleEndian.Uint64(scheduleBytes[i*8:])
	}
	for i := 4; i < 72; i++ {
		t := bits.RotateLeft64(k[i-1], -3) ^ k[i-3]
		t ^= bits.RotateLeft64(t, -1)
		k[i] = ^k[i-4] ^ t ^ ((uint64(0x3dc94c3a046d678b) >> ((i - 4) % 62)) & 1) ^ 3
	}
	encrypted := fanqiePad(pb)
	for i := 0; i < len(encrypted); i += 16 {
		x, y := binary.LittleEndian.Uint64(encrypted[i:]), binary.LittleEndian.Uint64(encrypted[i+8:])
		for _, round := range k {
			x, y = y, x^(bits.RotateLeft64(y, 1)&bits.RotateLeft64(y, 8))^bits.RotateLeft64(y, 2)^round
		}
		binary.LittleEndian.PutUint64(encrypted[i:], x)
		binary.LittleEndian.PutUint64(encrypted[i+8:], y)
	}
	mask := []byte{0xf2, 0xf7, 0xfc, 0xff, 0xf2, 0xf7, 0xfc, 0xff}
	wrapped := append(append([]byte(nil), mask...), encrypted...)
	for i := 8; i < len(wrapped); i++ {
		wrapped[i] ^= mask[i%8]
	}
	for l, r := 0, len(wrapped)-1; l < r; l, r = l+1, r-1 {
		wrapped[l], wrapped[r] = wrapped[r], wrapped[l]
	}
	wrapped = append(append([]byte{0xa6, 0x6e, 0xad, 0x9f, 0x77, 0x01, 0xd0, 0x0c, 0x18}, wrapped...), 0x61, 0x6f)
	aesKey, aesIV := md5.Sum(signKey[:16]), md5.Sum(signKey[16:])
	argus, err := fanqieCBC(wrapped, aesKey[:], aesIV[:])
	if err != nil {
		return nil, err
	}
	argus = append([]byte{0xf2, 0x81}, argus...)
	hash := md5.Sum(append(append([]byte(nil), random[4:]...), []byte(q.Get("aid"))...))
	ascii := []byte(hex.EncodeToString(hash[:]))
	var table [35]uint64
	table[0] = binary.LittleEndian.Uint64(ascii)
	queue := []uint64{binary.LittleEndian.Uint64(ascii[8:]), binary.LittleEndian.Uint64(ascii[16:]), binary.LittleEndian.Uint64(ascii[24:])}
	for i := 0; i < 34; i++ {
		v := (bits.RotateLeft64(queue[0], -8) + table[i]) ^ uint64(i)
		table[i+1] = v ^ bits.RotateLeft64(table[i], 3)
		queue = append(queue[1:], v)
	}
	ladon := fanqiePad([]byte(fmt.Sprintf("%d-%d-%d", ts, licenseID, aid)))
	for i := 0; i < len(ladon); i += 16 {
		x, y := binary.LittleEndian.Uint64(ladon[i:]), binary.LittleEndian.Uint64(ladon[i+8:])
		for _, round := range table[:34] {
			y = round ^ (x + bits.RotateLeft64(y, -8))
			x = y ^ bits.RotateLeft64(x, 3)
		}
		binary.LittleEndian.PutUint64(ladon[i:], x)
		binary.LittleEndian.PutUint64(ladon[i+8:], y)
	}
	return map[string]string{"x-argus": base64.StdEncoding.EncodeToString(argus), "x-ladon": base64.StdEncoding.EncodeToString(append(append([]byte(nil), random[4:]...), ladon...)), "x-khronos": strconv.FormatInt(ts, 10), "x-ss-req-ticket": strconv.FormatInt(ts*1000, 10)}, nil
}
