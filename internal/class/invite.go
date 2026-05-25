package class

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// newInviteCode 生成 12 位大写十六进制邀请码(6 字节熵)。
func newInviteCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}
