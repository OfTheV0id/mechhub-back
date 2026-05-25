package channel

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// clampLimit 把客户端传的 limit 收到合理区间。
func clampLimit(n int) int {
	if n <= 0 {
		return defaultPageLimit
	}
	if n > maxPageLimit {
		return maxPageLimit
	}
	return n
}
