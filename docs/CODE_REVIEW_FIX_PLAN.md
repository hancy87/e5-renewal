# E5 Renewal 코드 리뷰 수정 계획

## 배경

프로젝트 전체를 종합적으로 코드 리뷰한 결과, 심각한 문제 2개, 높은 위험도의 문제 5개, 중간 위험도의 문제 9개 이상을 발견했다. 이 문서는 프로젝트에 남겨 둘 수정 방안 참고 자료이자, 단계별로 실행할 수 있는 구현 계획이다.

## 구현 제약 사항

1. **SQLite 드라이버 교체 금지**: 순수 Go 구현인 `github.com/glebarez/sqlite`를 사용해야 하며, CGO가 필요한 `gorm.io/driver/sqlite` 등의 버전으로 교체해서는 안 된다. 프로젝트는 distroless 이미지를 사용하므로 정적 컴파일과 최소 이미지 크기를 보장해야 한다.
2. **모든 코드 주석은 영어로 작성**: 새로 추가하거나 수정하는 주석, godoc, JSDoc, TODO 등은 모두 영어로 작성한다.

## 구현 전략

4개 반복 작업으로 나누어 우선순위가 높은 순서대로 진행한다. 각 작업을 마칠 때마다 전체 테스트를 실행해 검증한다.

---

## 1차 작업: 보안 핵심 수정(심각 + 높음 보안 문제)

### 1.1 XSS 삽입 — 이스케이프하지 않은 `pathPrefix`
- **파일**: `backend/spa/handler.go:61`
- **문제**: `pathPrefix`를 `<script>` 태그에 직접 이어 붙여 환경 변수를 통한 XSS 삽입이 가능하다.
- **수정**: `json.Marshal`을 사용해 `pathPrefix`를 안전하게 인코딩한다.

```go
// handler.go buildIndexHTML(), line 61
// Before:
injection := `<script>window.__E5_CONFIG__={"pathPrefix":"` + pathPrefix + `"}</script>`
// After:
safePrefix, _ := json.Marshal(pathPrefix)
injection := `<script>window.__E5_CONFIG__={"pathPrefix":` + string(safePrefix) + `}</script>`
```
import 목록에 `"encoding/json"`을 추가해야 한다.

### 1.2 `math/rand` 동시성 데이터 경쟁
- **파일**: `backend/main.go:36`
- **문제**: 여러 고루틴에서 공유하는 `*rand.Rand`를 안전하지 않게 사용한다.
- **수정**: Executor와 Scheduler에 각각 독립적인 `*rand.Rand`를 생성한다.

```go
// main.go, lines 36-39
// Before:
rng := rand.New(rand.NewSource(time.Now().UnixNano()))
exec := executor.New(oauthSvc, rng)
sched := scheduler.New(exec, rng)
// After:
execRng := rand.New(rand.NewSource(time.Now().UnixNano()))
schedRng := rand.New(rand.NewSource(time.Now().UnixNano() + 1))
exec := executor.New(oauthSvc, execRng)
sched := scheduler.New(exec, schedRng)
```

### 1.3 OAuth `postMessage`가 `'*'`로 토큰을 브로드캐스트
- **파일**: `backend/handlers/oauth.go:111`
- **문제**: OAuth 토큰을 모든 창에 브로드캐스트한다.
- **수정**: `prefix` 매개변수를 `oauthResultHTML`에 전달해 올바른 origin을 구성하고, 프런트엔드에서도 `e.origin`을 검증한다.

백엔드 `oauth.go`:
```go
// Change oauthResultHTML signature to accept origin.
func oauthResultHTML(status, payload, origin string) []byte {
    safeOrigin, _ := json.Marshal(origin)
    // ...
    // Change line 111:
    // Before: window.opener.postMessage({ type: 'e5-oauth-result', data: result }, '*');
    // After: window.opener.postMessage({ type: 'e5-oauth-result', data: result }, %s);
    // %s is safeOrigin.
```

콜백 핸들러는 origin을 전달해야 한다.
```go
// Build origin: scheme + "://" + host.
scheme := "http"
if c.Request.TLS != nil {
    scheme = "https"
}
origin := fmt.Sprintf("%s://%s", scheme, c.Request.Host)
```

프런트엔드 `AccountFormDialog.vue`의 postMessage 리스너에 origin 검증을 추가한다.
```typescript
// Add at the start of the oauthListener callback:
if (e.origin !== window.location.origin) return
```

### 1.4 GET 쿼리 매개변수로 `client_secret` 전달
- **파일**: `backend/handlers/oauth.go:61-63`
- **문제**: GET 매개변수가 로그, 브라우저 기록, 프록시에 남는다.
- **수정**: `/authorize`를 POST 방식으로 바꾸고 요청 본문에서 값을 읽는다.

```go
// Before: authGroup.GET("/authorize", func(c *gin.Context) {
// After: authGroup.POST("/authorize", func(c *gin.Context) {
//   var req struct {
//       ClientID     string `json:"client_id" binding:"required"`
//       ClientSecret string `json:"client_secret"`
//       TenantID     string `json:"tenant_id" binding:"required"`
//   }
//   if err := c.ShouldBindJSON(&req); err != nil { ... }
```

프런트엔드 `AccountFormDialog.vue`의 해당 API 호출도 GET에서 POST로 변경해야 한다.

### 1.5 프런트엔드에 401 응답 인터셉터 없음
- **파일**: `frontend/src/api/client.ts`
- **문제**: 토큰이 만료되면 사용자에게 빈 페이지가 표시된다.
- **수정**: 응답 인터셉터를 추가한다.

```typescript
// Append to client.ts:
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      const { clearAuth } = useAuth()
      clearAuth()
      window.location.href = `${pathPrefix}/login`
    }
    return Promise.reject(error)
  }
)
```

### 1.6 프런트엔드 postMessage 리스너에서 origin을 검증하지 않음
- **파일**: `frontend/src/components/AccountFormDialog.vue:590`
- **수정**: 1.3과 같이 리스너 콜백 시작 부분에 `if (e.origin !== window.location.origin) return`을 추가한다.

---

## 2차 작업: 동시성 및 리소스 관리 수정(높음)

### ~~2.1 Scheduler WaitGroup 경쟁 상태~~ — 제거됨(오탐)
> 다시 검토한 결과, 현재 코드는 `t.Stop()`이 `true`를 반환할 때만 보정용 `wg.Done()`을 호출한다. 이는 올바른 Go 관용 패턴이며 이중 Done 패닉은 발생하지 않는다.

### 2.2 알림 설정 중복 로드
- **파일**: `backend/services/scheduler/scheduler.go:286-356`
- **문제**: `checkAuthExpiry`와 `notifyIfEnabled`가 각각 설정을 로드하므로 TOCTOU 문제가 있다.
- **수정**: `notifyIfEnabled`가 이미 로드된 config를 받도록 하여 중복 로드를 없앤다.

```go
func (s *Scheduler) notifyIfEnabled(cfg *models.NotificationConfig, eventKey, title, message string) {
    if s.Notifier == nil || cfg.URL == "" {
        return
    }
    // ... Use the supplied cfg to check whether the event is enabled.
}
```

모든 호출 지점에서 이미 로드된 `cfg`를 전달한다.

---

## 3차 작업: 중간 위험도 문제 수정

### 3.1 `gin.Default()` 대신 `gin.New()` 사용
- **파일**: `backend/main.go:41`
- **수정**: `gin.New()`을 사용하고 사용자 정의 미들웨어를 명시적으로 등록한다.

```go
r := gin.New()
r.Use(middleware.SlogLogger(), middleware.SlogRecovery())
```

### 3.2 프런트엔드 라우터에 catch-all 및 루트 경로 리디렉션 누락
- **파일**: `frontend/src/router/index.ts`
- **수정**:

```typescript
return [
    { path: '/login', component: LoginView, meta: { guest: true } },
    { path: '/', redirect: '/dashboard' },  // Add
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: '/dashboard', component: DashboardView },
        // ...
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },  // Add catch-all
]
```

### 3.3 요청 속도 제한기의 IP 위조 가능성
- **파일**: `backend/main.go`
- **수정**: 신뢰할 수 있는 프록시 설정을 추가한다.

```go
r := gin.New()
r.SetTrustedProxies(nil) // Or read from configuration.
```

### ~~3.4 데이터베이스 디렉터리 권한~~ — 제거됨(오탐)
> 다시 검토한 결과, `0755`는 디렉터리의 표준 권한(rwxr-xr-x)이므로 지나치게 느슨한 권한이 아니다.

### 3.5 HTTP 연결 재사용 저하
- **파일**: `backend/services/graph/caller.go:260-263`
- **수정**: body를 읽은 뒤 남은 데이터를 비운다.

```go
bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
io.Copy(io.Discard, resp.Body) // Drain the remaining body.
```

### 3.6 `DeleteCascade`가 Pluck 오류를 무시함
- **파일**: `backend/database/account.go:67-84`
- **수정**: Pluck이 반환한 오류를 확인한다.

```go
if err := tx.Model(&models.TaskLog{}).Where("account_id = ?", id).Pluck("id", &logIDs).Error; err != nil {
    return err
}
```

### 3.7 ConfirmDialog 기본 버튼 문구가 중국어로 하드코딩됨
- **파일**: `frontend/src/components/ConfirmDialog.vue:69-72`
- **문제**: 기본값 `'确定'` / `'取消'`가 중국어로 하드코딩되어 있지만, i18n에는 이미 `confirm.ok` / `confirm.cancel` 키가 있다.
- **수정**: 기존 i18n 키를 사용한다.

```typescript
import { useI18n } from '../i18n'
const { t } = useI18n()
// In withDefaults:
confirmText: t('confirm.ok'),
cancelText: t('confirm.cancel'),
```

### 3.8 누락된 i18n 키 추가
- **파일**: `frontend/src/i18n/index.ts`
- 다음 누락 키를 zh와 en에 추가한다.
  - `accounts.form.refreshToken.oauth.parseError`
  - `accounts.form.refreshToken.oauth.failed`

### 3.9 DashboardView statCards 반응성 문제
- **파일**: `frontend/src/views/DashboardView.vue:483-486`
- **문제**: `computed(() => ...).value`가 즉시 평가되어 반응성을 잃는다.
- **수정**: `valueClass`를 함수 호출로 변경한다.

```typescript
// Before:
valueClass: computed(() => data.value.error_count > 0 ? 'text-red-500' : '...').value
// After:
valueClass: () => data.value.error_count > 0 ? 'text-red-500' : '...'
```
템플릿의 해당 부분은 `:class="card.valueClass?.()"`로 변경한다.

### 3.10 AppSidebar의 isDark가 시스템 테마 변경에 반응하지 않음
- **파일**: `frontend/src/components/AppSidebar.vue:137`
- **수정**: MutationObserver를 사용해 class 변경을 감시한다.

```typescript
const isDark = ref(document.documentElement.classList.contains('dark'))
const observer = new MutationObserver(() => {
  isDark.value = document.documentElement.classList.contains('dark')
})
onMounted(() => observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] }))
onUnmounted(() => observer.disconnect())
```

### 3.11 모든 중국어 코드 주석을 영어로 변경
- **대상 파일**:
  - `backend/spa/handler.go` — 중국어 주석 6곳
  - `backend/handlers/oauth.go` — 3곳
  - `backend/services/executor/executor.go` — 4곳
  - `backend/services/scheduler/scheduler.go` — 1곳
  - `backend/services/oauth/state.go` — 3곳
  - `backend/services/graph/caller.go` — 1곳
  - `backend/middleware/ratelimit.go` — 1곳
  - `backend/models/models.go` — 1곳
  - `backend/services/security/crypto_test.go` — 1곳
  - `backend/handlers/account_test.go` — 5곳
  - `backend/services/oauth/state_test.go` — 1곳
  - `frontend/src/main.ts` — 2곳
  - `frontend/src/config.ts` — 1곳
  - `frontend/src/views/AccountsView.vue` — 1곳
  - `frontend/src/components/AccountFormDialog.vue` — 2곳
- **수정**: 각 주석을 의미가 같은 영어 주석으로 번역한다. 테스트 파일에서 i18n 중국어 텍스트를 인용하는 주석은 원문 인용을 유지하되 설명은 영어로 작성한다.

### 3.12 Vite 빌드 base를 상대 경로로 변경하고 handler.go의 문자열 치환 우회 코드 제거
- **파일**: `frontend/vite.config.ts`, `backend/spa/handler.go:64-68`
- **문제**: 현재 Vite의 `base` 기본값은 `"/"`이므로 빌드 결과물의 리소스 경로가 절대 경로(`/assets/xxx.js`)가 된다. 백엔드 `handler.go`는 `bytes.ReplaceAll`을 사용해 index.html의 `"/assets/` 및 `"/favicon`에 pathPrefix를 붙이지만, 이 방식은 취약하고 불완전하다.
  - `"/assets/`와 `"/favicon`만 처리하므로 새 리소스 경로가 추가되면 누락된다.
  - CSS 파일 안의 `url()` 참조(글꼴, 이미지 등)는 치환되지 않는다.
  - Vite 빌드 출력 구조가 바뀌면 치환 로직이 아무 경고 없이 실패한다.
- **수정**:
  1. `vite.config.ts`에 `base: "./"`를 추가한다.
     ```typescript
     export default defineConfig({
       base: './',
       // ...
     })
     ```
  2. `handler.go`의 `ReplaceAll` 치환 로직(64~68행)을 삭제한다.
     ```go
     // Delete the following code:
     if pathPrefix != "" {
         result = bytes.ReplaceAll(result, []byte(`"/assets/`), []byte(`"`+pathPrefix+`/assets/`))
         result = bytes.ReplaceAll(result, []byte(`"/favicon`), []byte(`"`+pathPrefix+`/favicon`))
     }
     ```
  3. Vue Router와 API의 base path는 이미 런타임에 `window.__E5_CONFIG__.pathPrefix`로 처리되므로 추가 변경은 필요 없다.
  4. 검증: pathPrefix가 있는 모드와 없는 모드로 각각 실행해 정적 리소스가 정상적으로 로드되는지 확인한다.

---

## 4차 작업: 낮은 우선순위 개선

### 4.1 JWT에 Issuer 클레임 추가
- **파일**: `backend/services/security/jwt.go`
- Claims에 `Issuer: "e5-renewal"`을 추가하고 `ParseJWT`에서 검증한다.

### 4.2 CI의 golangci-lint를 v2로 업그레이드
- **파일**: `.github/workflows/ci.yml:30`, `backend/.golangci.yml`
- `version: latest`를 v2 안정 버전(예: `version: v2.1.0`)으로 고정한다.
- `.golangci.yml` 설정 형식 변경에도 맞춰야 한다(v2에는 호환성을 깨는 변경 사항이 있음).

### 4.3 Dependabot에 Docker 생태계 추가
- **파일**: `.github/dependabot.yml`
- `package-ecosystem: docker`를 추가한다.

### 4.4 login/store.go에서 저장소 계층 사용
- **파일**: `backend/services/login/store.go:73, 84`
- 직접 DB를 호출하는 코드를 `database.Settings.Get()`과 `database.Settings.Upsert()`를 거치도록 변경한다.

---

## 검증 절차

각 작업을 마친 뒤 다음 명령어를 실행한다.

```bash
# 백엔드
cd backend && go test -race ./... && golangci-lint run

# 프런트엔드
cd frontend && npx vitest run && npx eslint src/

# 전체 빌드
docker build -t e5-renewal:test .
```

## 주요 파일 목록

| 파일 | 작업 | 변경 유형 |
|------|------|----------|
| `backend/spa/handler.go` | 1, 3 | XSS 수정 + ReplaceAll 우회 코드 삭제 |
| `backend/main.go` | 1, 3 | 동시성 수정 + gin.New() |
| `backend/handlers/oauth.go` | 1 | 보안 수정(POST + postMessage origin) |
| `frontend/src/api/client.ts` | 1 | 401 인터셉터 추가 |
| `frontend/src/components/AccountFormDialog.vue` | 1 | origin 검증 + API를 POST로 변경 |
| `backend/services/scheduler/scheduler.go` | 2 | 알림 설정 중복 로드 수정 |
| `frontend/src/router/index.ts` | 3 | catch-all + 리디렉션 |
| `frontend/src/views/DashboardView.vue` | 3 | 반응성 수정 |
| `frontend/src/components/AppSidebar.vue` | 3 | 다크 모드 감시 |
| `frontend/src/components/ConfirmDialog.vue` | 3 | i18n 기본 문구 |
| `frontend/src/i18n/index.ts` | 3 | 누락된 키 추가 |
| `frontend/vite.config.ts` | 3 | base를 `"./"`로 변경 |
| `backend/database/account.go` | 3 | Pluck 오류 검사 |
| `backend/services/graph/caller.go` | 3 | body 비우기 |
| `backend/services/security/jwt.go` | 4 | issuer 클레임 |
| `.github/workflows/ci.yml` | 4 | golangci-lint v2 업그레이드 |
| `backend/.golangci.yml` | 4 | v2 설정 형식에 맞게 변경 |
| `backend/services/login/store.go` | 4 | 저장소 계층 적용 |
