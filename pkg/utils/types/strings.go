package types

import "strings"

func DeepCopyString(s string) string {
	var sb strings.Builder
	sb.WriteString(s)
	return sb.String()
}
