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

func IsGroupControlTopic(topic string) bool {
	return strings.EqualFold(strings.TrimSpace(topic), GroupControlTopicV1)
}

func IsGroupMessageTopic(topic string) bool {
	return strings.EqualFold(strings.TrimSpace(topic), GroupMessageTopicV1)
}
