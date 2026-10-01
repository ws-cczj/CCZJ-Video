package download

import (
	"cczjVideo/app/apperror"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
)

// SegmentIV 给出片段生效密钥的 IV：显式 IV 原样用，否则按 RFC 8216 取片段序号的
// 128 位大端表示。
func SegmentIV(key *Key, seq int64) []byte {
	if key != nil && len(key.IV) == aes.BlockSize {
		out := make([]byte, aes.BlockSize)
		copy(out, key.IV)
		return out
	}
	out := make([]byte, aes.BlockSize)
	binary.BigEndian.PutUint64(out[8:], uint64(seq))
	return out
}

// DecryptAES128 解 HLS 的 AES-128-CBC 分片。HLS 的分片一定是整块数、且带 PKCS#7
// 填充，两条都不满足说明拿到的不是这个密钥能解的东西 —— 报出来，别把垃圾字节写进
// 用户的文件。
func DecryptAES128(key, iv, ciphertext []byte) ([]byte, error) {
	if len(key) != aes.BlockSize {
		return nil, apperror.Newf(apperror.Corrupt, "AES-128 密钥应为 %d 字节，实际 %d", aes.BlockSize, len(key))
	}
	if len(iv) != aes.BlockSize {
		return nil, apperror.Newf(apperror.Corrupt, "AES-128 IV 应为 %d 字节，实际 %d", aes.BlockSize, len(iv))
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, apperror.Newf(apperror.Corrupt, "分片长度 %d 不是 AES 块的整数倍", len(ciphertext))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	return pkcs7Unpad(out)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, apperror.Newf(apperror.Corrupt, "PKCS#7 填充长度无效: %d", pad)
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, apperror.New(apperror.Corrupt, "PKCS#7 填充内容不一致")
		}
	}
	return data[:len(data)-pad], nil
}
