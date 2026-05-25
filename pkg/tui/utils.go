package tui

import "fmt"

// getString 从 map 中获取字符串
func getString(data map[string]interface{}, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}

// getFloat 从 map 中获取 float64
func getFloat(data map[string]interface{}, key string) float64 {
	if val, ok := data[key].(float64); ok {
		return val
	}
	return 0
}

// getBool 从 map 中获取 bool
func getBool(data map[string]interface{}, key string) bool {
	if val, ok := data[key].(bool); ok {
		return val
	}
	return false
}

// formatBytes 将字节数格式化为人类可读的形式
func formatBytes(v interface{}) string {
	var bytes float64
	switch b := v.(type) {
	case float64:
		bytes = b
	case int:
		bytes = float64(b)
	case int64:
		bytes = float64(b)
	case uint64:
		bytes = float64(b)
	default:
		return fmt.Sprintf("%v", v)
	}

	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", bytes/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", bytes/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", bytes/KB)
	default:
		return fmt.Sprintf("%.0f B", bytes)
	}
}
