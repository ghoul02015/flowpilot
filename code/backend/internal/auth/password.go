// Package auth 提供单管理员认证：Argon2id 密码哈希、服务端会话、
// 登录限流与操作审计。会话与审计记录存于数据底座 SQLite。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// AdminUsername 阶段 1 固定单管理员。
	AdminUsername = "admin"
	// argon2id 标准参数：64MB 内存、1 轮、4 线程（服务器场景平衡值）。
	argonMemory  = 64 * 1024
	argonTime    = 1
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

// ErrInvalidCredentials 统一的登录失败原因（不区分用户名/密码错误，防枚举）。
var ErrInvalidCredentials = errors.New("用户名或密码错误")

// HashPassword 生成 Argon2id 编码哈希：$argon2id$v=19$m=65536,t=1,p=4$salt$hash。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐失败：%w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword 常数时间校验密码与编码哈希；格式非法一律失败。
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// randomToken 生成 cryp-to 随机 hex 令牌。
func randomToken(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		// rand 失败在服务进程属致命环境问题，直接 panic 由上层恢复。
		panic(fmt.Sprintf("crypto/rand 失败：%v", err))
	}
	return hex.EncodeToString(buffer)
}

// ValidatePasswordPolicy 密码策略：≥8 位且同时含字母与数字。
func ValidatePasswordPolicy(password string) error {
	if len(password) < 8 {
		return errors.New("密码至少 8 位")
	}
	hasLetter, hasDigit := false, false
	for _, r := range password {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("密码需同时包含字母与数字")
	}
	return nil
}
