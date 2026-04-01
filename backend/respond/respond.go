package respond

import "github.com/gin-gonic/gin"

// Error returns a bilingual error payload with Korean as the default message.
// Error는 기본 메시지를 한국어로 제공하는 이중 언어 오류 응답 본문을 반환합니다.
func Error(ko, en string) gin.H {
	return gin.H{"error": ko, "error_en": en}
}

// Status returns a bilingual status payload with Korean as the default message.
// Status는 기본 메시지를 한국어로 제공하는 이중 언어 상태 응답 본문을 반환합니다.
func Status(ko, en string) gin.H {
	return gin.H{"status": ko, "status_en": en}
}

// Message returns a bilingual human-readable message payload.
// Message는 사람이 읽을 수 있는 이중 언어 메시지 응답 본문을 반환합니다.
func Message(ko, en string) gin.H {
	return gin.H{"message": ko, "message_en": en}
}

// Merge combines one or more JSON payloads into a single response object.
// Merge는 하나 이상의 JSON 응답 본문을 단일 객체로 결합합니다.
func Merge(parts ...gin.H) gin.H {
	merged := gin.H{}
	for _, part := range parts {
		for key, value := range part {
			merged[key] = value
		}
	}
	return merged
}
