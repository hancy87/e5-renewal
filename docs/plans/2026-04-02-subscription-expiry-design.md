# Microsoft 365 구독 만료일 표시 설계

## 배경
현재 프로젝트는 계정별 `auth_expires_at` 필드를 통해 인증 자격 만료일을 관리하고 있으며, 프론트엔드에서는 이를 **클라이언트 시크릿 만료일**로 표시한다. 하지만 사용자는 Microsoft 365 Developer Profile 페이지에서 확인하는 **구독 만료일**도 함께 보고 싶어 한다.

이번 변경의 목표는 기존 인증 만료일 흐름을 유지하면서, 별도의 **구독 만료일**을 수동 입력하고 Dashboard/Accounts/계정 수정 모달에서 함께 확인할 수 있도록 만드는 것이다.

## 확정된 요구사항
- 자동 수집은 이번 범위에서 제외한다.
- Microsoft 365 Developer Profile 기준의 구독 만료일은 **수동 입력**으로 관리한다.
- 저장 형식은 남은 일수 숫자가 아니라 **만료일(날짜)** 이다.
- 표시 위치는 다음 세 곳이다.
  - Dashboard
  - Accounts 목록
  - 계정 수정 모달
- 기존 `auth_expires_at` 는 유지하며, 의미는 **클라이언트 시크릿 만료일**로 둔다.
- 새 필드는 계정(Account)에 직접 추가한다.

## 용어 정리
두 필드는 서로 다른 의미를 가진다.

- `auth_expires_at`: 클라이언트 시크릿 또는 인증 자격 만료일
- `subscription_expires_at`: Microsoft 365 개발자 구독 만료일

이 구분이 흐려지면 사용자가 잘못된 날짜를 입력할 수 있으므로, UI 라벨과 설명 문구는 반드시 명확히 분리해야 한다.

## 권장 접근
가장 적합한 방식은 `Account` 모델에 `subscription_expires_at` 필드를 추가하는 것이다.

이 접근을 선택한 이유:
- 기존 `auth_expires_at` 처리 패턴을 재사용할 수 있다.
- Accounts 화면과 계정 수정 모달에 가장 자연스럽게 붙는다.
- Dashboard 요약도 적은 변경으로 확장 가능하다.
- 별도 엔티티를 만들지 않아도 현재 요구사항을 충분히 만족한다.

대안 검토:
- 별도 Subscription/Profile 엔티티 추가: 현재 범위 대비 과한 구조 확장
- 전역 Settings에 만료일 하나만 저장: 계정별 표시 요구와 맞지 않음

## 데이터 모델 설계
### 백엔드 모델
`backend/models/models.go` 의 `Account` 모델에 nullable 날짜 필드를 추가한다.

예상 필드:
- `SubscriptionExpiresAt *time.Time`

JSON 응답/요청에서는 기존 패턴을 따라 문자열 필드로 주고받는다.

예상 API 필드:
- `subscription_expires_at: string`

형식:
- `YYYY-MM-DD`
- 비어 있으면 미설정으로 간주

## API 설계
수정 대상은 기존 계정 API이다.

### 요청/응답 확장
`backend/handlers/account.go` 의 다음 구조체에 필드를 추가한다.
- `accountRequest`
- `accountResponse`

추가 필드:
- `subscription_expires_at`

### 생성/수정 동작
- 값이 있으면 `2006-01-02` 형식으로 파싱해 저장
- 빈 문자열이면 nil 또는 미설정으로 저장
- 형식이 잘못되면 400 응답
- 과거 날짜는 허용하고, 표시만 “이미 만료됨”으로 처리

### Dashboard 응답 확장
새 엔드포인트를 만들기보다는 기존 dashboard summary 응답을 확장한다.

후보 필드:
- `expiring_subscription_count`
- `expired_subscription_count`
- `nearest_subscription_expiry`

권장 기본안:
- `expired_subscription_count`
- `expiring_subscription_count` (예: 30일 이내)
- `nearest_subscription_expiry`

이렇게 하면 운영자가 Dashboard에서 구독 상태를 즉시 파악할 수 있다.

## 프론트엔드 설계
### 1. Accounts 목록
현재 Accounts 화면에는 `auth_expires_at` 를 기반으로 한 만료일 블록이 이미 있다. 여기에 **구독 만료일 블록**을 추가한다.

표시 원칙:
- 인증 만료일과 구독 만료일을 한 라인에 섞지 않는다.
- 각 항목은 별도 라벨을 갖는다.
- 각 항목은 `날짜 + 상태 텍스트` 구조를 가진다.

예시:
- 클라이언트 시크릿 만료일: `2026-05-01 · 12일 남음`
- 구독 만료일: `2026-06-15 · 57일 남음`
- 미설정: `미설정`
- 이미 만료됨: `이미 만료됨`

색상 규칙은 기존 만료일 로직을 재사용한다.

### 2. 계정 수정 모달
`AccountFormDialog.vue` 에 입력 필드를 하나 더 추가한다.

권장 라벨:
- 클라이언트 시크릿 만료일
- Microsoft 365 구독 만료일

권장 보조 문구:
- Developer Profile 페이지에서 확인한 구독 만료일을 입력하세요.

중요 포인트:
- 두 입력 필드는 가까운 위치에 두되, 설명 문구로 의미를 분리한다.
- 사용자가 두 날짜를 혼동하지 않도록 해야 한다.

### 3. Dashboard
Dashboard에는 운영 관점의 요약 카드 하나를 추가한다.

권장 내용:
- 가장 임박한 구독 만료일
또는
- 30일 이내 만료 계정 수

최소 구현으로는 다음 2개가 가장 실용적이다.
- 30일 이내 만료 계정 수
- 이미 만료된 계정 수

필요 시 보조 텍스트로 가장 가까운 만료일 날짜를 추가한다.

## 표시 로직
구독 만료일도 기존 `auth_expires_at` 표시 로직과 동일한 계산 방식을 쓴다.

규칙:
- 날짜가 없음 → `미설정`
- 남은 일수 < 0 → `이미 만료됨`
- 남은 일수 = 0 → `오늘 만료`
- 남은 일수 > 0 → `{days}일 남음`

색상 권장:
- 30일 초과: 중립
- 7일 이내: 경고
- 만료: 위험

공통 헬퍼를 재사용하거나 만료일 계산 유틸을 추출해 중복을 줄인다.

## 오류 처리 원칙
- 잘못된 날짜 형식은 저장 시점에 명확히 차단한다.
- 과거 날짜는 허용한다. 운영자가 이미 만료된 상태를 기록할 수 있어야 한다.
- 미설정은 정상 상태로 취급한다.
- 자동 수집 실패 처리나 외부 API 예외 처리는 이번 범위에 포함하지 않는다.

## 테스트 전략
### 백엔드
추가/수정 대상:
- `backend/handlers/account_test.go`
- `backend/handlers/dashboard_test.go`
- 필요 시 `backend/database/account_test.go`

검증 항목:
- `subscription_expires_at` 저장/조회가 정상 동작하는지
- 빈 문자열 입력 시 미설정으로 처리되는지
- `auth_expires_at` 와 독립적으로 유지되는지
- Dashboard 집계가 의도대로 계산되는지

### 프론트엔드
추가/수정 대상:
- `frontend/src/__tests__/AccountsView.spec.ts`
- `frontend/src/__tests__/AccountFormDialog.spec.ts`
- `frontend/src/__tests__/DashboardView.spec.ts`
- 필요 시 `frontend/src/__tests__/i18n.spec.ts`

검증 항목:
- Accounts 목록에 구독 만료일이 표시되는지
- 미설정/오늘 만료/이미 만료/남은 일수 상태가 올바르게 노출되는지
- 계정 수정 모달에서 새 필드를 입력/수정할 수 있는지
- Dashboard에 구독 요약 카드가 보이는지
- 기존 클라이언트 시크릿 만료일 UI가 깨지지 않는지

## 구현 순서
1. Account 모델과 DB 마이그레이션 반영
2. 계정 API 요청/응답에 `subscription_expires_at` 추가
3. Dashboard summary 응답 확장
4. i18n 문구 추가
5. AccountFormDialog에 입력 필드 추가
6. Accounts 목록에 구독 만료일 블록 추가
7. Dashboard 카드 추가
8. 백엔드/프론트 테스트 보강
9. lint, typecheck, test 검증

## 성공 기준
- 사용자는 계정별로 Microsoft 365 구독 만료일을 수동 입력할 수 있다.
- Accounts 목록에서 인증 만료일과 구독 만료일을 서로 구분해서 볼 수 있다.
- Dashboard에서 구독 만료 위험을 빠르게 파악할 수 있다.
- 기존 `auth_expires_at` 동작은 유지된다.
- 두 만료일의 의미가 UI에서 명확히 구분된다.

## 이번 범위에서 제외
- Microsoft Developer Profile 자동 수집
- 브라우저 스크래핑
- 로그인 세션 연동
- 외부 페이지 구조 변화 대응
- 구독 만료일 기반 자동 알림 추가

## 후속 확장 가능성
향후 필요하면 다음을 별도 작업으로 확장할 수 있다.
- 구독 만료일 기반 알림 추가
- 자동 수집 시도 + 수동 입력 대체 방식
- Dashboard에서 만료일 기준 정렬/필터
- 구독 만료일 변경 이력 추적
