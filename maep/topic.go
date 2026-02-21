package maep

import "strings"

func IsDialogueTopic(topic string) bool {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "share.proactive.v1", "dm.checkin.v1", "dm.reply.v1", "chat.message":
		return true
	default:
		return false
	}
}

func SessionScopeByTopic(topic string) string {
	t := strings.ToLower(strings.TrimSpace(topic))
	switch {
	case IsDialogueTopic(t):
		return "dialogue.v1"
	case t == "":
		return "default"
	default:
		return "topic:" + t
	}
}
