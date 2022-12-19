# emojicleaner

## Usage

```shell
$ export SLACK_BOT_TOKEN=xoxp-...
# 슬랙 채널 & 이모지 & 메시지 다운로드 (기본 최근 30일치 메시지만 사용)
$ go run -v -race cmd/download/main.go
# 오랫동안 사용되지 않은 이모지 추출
$ go run -v -race cmd/stale/main.go
```

## Troubleshooting

### `not_authed` 에러

- `SLACK_BOT_TOKEN` 환경변수 세팅 필요
- [Slack App Directory 페이지](https://api.slack.com/apps)의 앱 상세페이지에서 Features - OAuth & Permissions 메뉴에서 확인 가능
- `User OAuth Token`을 사용

### `GetEmoji: missing_scope` 에러

- User Token Scopes에 아래 권한 추가 필요
  - `channels:history`
  - `channels:read`
  - `emoji:read`
  - `users:read`
