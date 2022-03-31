package types

import "strings"

// DeepCopyString deep copy a string with new memory space
func DeepCopyString(s string) string {
	var sb strings.Builder
	sb.WriteString(s)
	return sb.String()
}
