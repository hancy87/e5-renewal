// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import { useI18n } from '../i18n'

describe('useI18n', () => {
  beforeEach(() => {
    localStorage.clear()
    const { setLocale } = useI18n()
    setLocale('ko')
  })

  it('기본 로케일은 한국어다', () => {
    const { locale } = useI18n()
    expect(locale.value).toBe('ko')
  })

  it('t()는 한국어 번역을 반환한다', () => {
    const { t } = useI18n()
    expect(t('nav.dashboard')).toBe('대시보드')
    expect(t('login.submit')).toBe('로그인')
    expect(t('settings.notification.title')).toBe('알림 설정')
  })

  it('없는 키는 그대로 반환한다', () => {
    const { t } = useI18n()
    expect(t('nonexistent.key')).toBe('nonexistent.key')
  })

  it('setLocale은 한국어를 유지하고 localStorage에 저장한다', () => {
    const { setLocale, locale } = useI18n()
    setLocale('ko')
    expect(locale.value).toBe('ko')
    expect(localStorage.getItem('locale')).toBe('ko')
  })

  it('주요 화면 문구가 한국어로 제공된다', () => {
    const { t } = useI18n()

    expect(t('accounts.expiry')).toBe('클라이언트 시크릿 만료일')
    expect(t('accounts.expiry.none')).toBe('클라이언트 시크릿 만료일이 설정되지 않았습니다')
    expect(t('accounts.expiry.expired')).toBe('클라이언트 시크릿이 만료되었습니다')
    expect(t('accounts.form.refreshToken.mode.auto')).toBe('자동 가져오기')
    expect(t('accounts.form.refreshToken.mode.manual')).toBe('수동 가져오기')
    expect(t('accounts.form.refreshToken.mode.direct')).toBe('직접 입력')
    expect(t('settings.notification.language')).toBe('알림 언어')
  })
})
