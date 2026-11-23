package auth

import "os"

// readTokenFile 读取令牌文件（测试用）。
func readTokenFile(path string) ([]byte, error) { return os.ReadFile(path) }
