package aqua

import "strings"

func IsDialogueTopic(topic string) bool {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "chat.message":
		return true
	default:
		return false
	}
}
