# CLAUDE.md

## 프로젝트 개요

E5 Renewal은 셀프 호스팅 방식의 Microsoft 365 E5 개발자 구독 갱신 도구입니다. 설정 가능한 일정에 따라 Graph API 호출을 자동화하여 E5 구독을 활성 상태로 유지합니다.

- **백엔드:** Go 1.25 + Gin 프레임워크 + SQLite(GORM)
- **프런트엔드:** Vue 3 + TypeScript + Tailwind CSS + Element Plus
- **배포:** 프런트엔드가 내장된 단일 Docker 이미지(distroless)

## 프로젝트 구조

```
e5-renewal/
├── backend/                 # Go 백엔드
│   ├── main.go              # 진입점 및 전체 컴포넌트 연결
│   ├── config/              # YAML + 환경 변수 설정 로더
│   ├── database/            # GORM 모델 및 저장소
│   ├── handlers/            # Gin HTTP 핸들러(REST API)
│   ├── middleware/          # 인증(JWT), 요청 속도 제한, 로깅
│   ├── models/              # 공유 데이터 구조
│   ├── services/
│   │   ├── executor/        # Graph API 호출 실행
│   │   ├── graph/           # Microsoft Graph API 클라이언트
│   │   ├── login/           # 로그인 키 관리(bcrypt)
│   │   ├── notifier/        # Shoutrrr 푸시 알림
│   │   ├── oauth/           # OAuth 2.0 흐름(인증 코드)
│   │   ├── scheduler/       # 타이머 기반 주기적 작업 예약
│   │   └── security/        # JWT, bcrypt, AES 암호화
│   ├── spa/                 # SPA 정적 파일 핸들러
│   └── static/dist/         # 내장 프런트엔드 빌드 결과물(자동 생성)
├── frontend/                # Vue 3 SPA
│   ├── src/
│   │   ├── components/      # 재사용 가능한 Vue 컴포넌트
│   │   ├── views/           # 페이지 단위 뷰
│   │   ├── api/             # API 클라이언트(axios)
│   │   ├── i18n/            # 국제화(zh-CN, en)
│   │   ├── router/          # Vue Router 설정
│   │   └── stores/          # Pinia 스토어(인증)
│   └── vite.config.ts
├── Dockerfile               # 다단계 빌드(node → go → distroless)
├── docker-compose.yml       # 로컬 배포 템플릿
└── e5-renewal.yaml.example  # 설정 파일 템플릿
```

## 주요 규칙

- **언어:** Go 코드는 표준 라이브러리 패턴을 따르고, 프런트엔드는 `<script setup>`을 사용하는 Vue 3 Composition API를 적용합니다.
- **데이터베이스:** 모든 DB 접근은 `database/` 패키지의 저장소 구조체를 거치며, 핸들러에서 원시 SQL을 사용하지 않습니다.
- **인증:** API 인증에는 JWT 토큰을 사용하고, 로그인 키는 bcrypt 해시로 DB에 저장합니다.
- **비밀 정보:** 클라이언트 시크릿과 갱신 토큰은 AES-GCM으로 암호화합니다(설정의 `encryption_key`).
- **설정 우선순위:** 환경 변수 > 설정 파일 > 기본값
- **오류 응답:** 항상 `gin.H{"error": "message"}` 형식을 사용합니다.
- **국제화:** 프런트엔드는 zh-CN과 en을 지원하고, 알림 메시지는 `notifier/messages.go`를 통해 두 언어를 모두 지원합니다.

## 개발 명령어

```bash
# 백엔드
cd backend
go build ./...              # 빌드
go test ./...               # 테스트 실행
go test -race ./...         # 경쟁 상태 감지기를 사용해 테스트 실행
golangci-lint run           # 린트(.golangci.yml 설정 참고)

# 프런트엔드
cd frontend
npm ci                      # 의존성 설치
npm run dev                 # 개발 서버
npm run build               # 프로덕션 빌드
npx eslint src/             # 린트
npx vitest run              # 테스트 실행

# Docker
docker build -t e5-renewal:latest .
```

## CI 요구사항

- 백엔드 테스트 커버리지 80% 이상
- 프런트엔드 테스트 커버리지 80% 이상
- ESLint 경고 20개 이하
- golangci-lint 통과(`.golangci.yml`의 엄격한 설정 적용)
- Docker 빌드 성공

## 중요 참고 사항

- 스케줄러는 정상 종료를 위해 컨텍스트 취소와 함께 `time.AfterFunc`를 사용합니다. 타이머 콜백에 WaitGroup 추적을 추가하지 마세요.
- `rateLimiter`에는 정리 고루틴을 위한 `Stop()` 메서드가 있으며, 테스트 편의를 위해 공개되어 있습니다.
- OAuth `/callback` 엔드포인트는 Microsoft의 리디렉션 대상이므로 인증 없이 접근할 수 있어야 합니다.
- `maskSecret` 함수는 업데이트 시 클라이언트가 마스킹된 값을 다시 보냈는지 감지하는 데 사용합니다. 패턴 매칭이 아니라 정확한 마스킹 결과를 비교하세요.
- 프런트엔드 빌드 결과물은 `embed.FS`를 통해 Go 바이너리에 내장됩니다.
