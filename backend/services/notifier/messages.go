package notifier

import "fmt"

// FormatTest는 테스트용 알림 제목과 본문을 포맷합니다.
// FormatTest returns a formatted test notification message.
func FormatTest(lang string) (title, message string) {
	if lang == "en" {
		return "E5 Renewal", "Test notification"
	}
	return "E5 Renewal", "테스트 알림"
}

// FormatAuthExpiry는 인증 만료 알림 제목과 본문을 포맷합니다.
// FormatAuthExpiry returns a formatted auth expiry notification.
func FormatAuthExpiry(lang, accountName string, daysLeft int) (title, message string) {
	if lang == "en" {
		if daysLeft < 0 {
			return "Client Secret Expired",
				fmt.Sprintf("Account: %s\nExpired %d days ago", accountName, -daysLeft)
		}
		return "Client Secret Expiring",
			fmt.Sprintf("Account: %s\nExpires in %d days", accountName, daysLeft)
	}
	if daysLeft < 0 {
		return "클라이언트 시크릿이 만료되었습니다",
			fmt.Sprintf("계정: %s\n%d일 전에 만료되었습니다", accountName, -daysLeft)
	}
	return "클라이언트 시크릿 만료 알림",
		fmt.Sprintf("계정: %s\n%d일 후 만료됩니다", accountName, daysLeft)
}

// FormatTaskAllFailed는 모든 작업 실패 알림 제목과 본문을 포맷합니다.
// FormatTaskAllFailed returns a formatted all-tasks-failed notification.
func FormatTaskAllFailed(lang, accountName string, failCount int) (title, message string) {
	if lang == "en" {
		return "All Tasks Failed",
			fmt.Sprintf("Account: %s\nFailed: %d endpoints", accountName, failCount)
	}
	return "모든 작업이 실패했습니다",
		fmt.Sprintf("계정: %s\n실패: %d개 엔드포인트", accountName, failCount)
}

// FormatHealthLow는 낮은 건강도 알림 제목과 본문을 포맷합니다.
// FormatHealthLow returns a formatted health-low notification.
func FormatHealthLow(lang, accountName string, health float64, threshold int) (title, message string) {
	if lang == "en" {
		return "Health Low",
			fmt.Sprintf("Account: %s\nCurrent: %.0f%%\nThreshold: %d%%", accountName, health, threshold)
	}
	return "건강도가 낮습니다",
		fmt.Sprintf("계정: %s\n현재: %.0f%%\n임계값: %d%%", accountName, health, threshold)
}
