# AGENTS.md

本仓是 `*2api-panel` 家族的新成员：MonkeyCode（长亭百智云）→ OpenAI / Claude / Codex 三协议反代。

## 红线

- **协议结论必须实测**。上游是 Agent 任务平台（建任务 + WebSocket ACP 事件），任何「看起来对」的
  字段名都要用真实账号跑通再写进代码；猜出来的映射一律标注未验证。
- **不要杜撰 token**。上游没给 usage 就报 0，不要按字符数估算后写进 `usage`。
- **失败必须显式收尾**：流式路径要么给 `[DONE]` / `response.completed` / `message_stop`，要么给错误帧，
  不能只断连接（客户端会渲染成「回答被截断」）。
- **一个账号同时只跑一个任务**（上游 `10811`）。池里忙就换号，不要排队把延迟拖到几分钟。
- **不要把 agent 的工具调用当客户端的 function call 下发**：那是沙箱内的内部动作，客户端没有对应
  的工具定义，下发只会让客户端报错。
- 日志不记正文、不记 session；`auths/`、`data/`、`config.json` 不进 git。

## 验证

```bash
go vet ./... && go test ./...
go build -o mc2api ./cmd/server
./mc2api -config config.json &
curl -s localhost:7887/healthz
curl -s -H "Authorization: Bearer $MC2A_API_KEY" localhost:7887/v1/models
```

端到端需要真实 session（`./login.sh`），三套协议各跑一次流式 + 非流式。

## 结构约定

- `internal/upstream` 只做上游：REST、WebSocket、ACP 解析、模型目录。不做协议编解码。
- `internal/server/run.go` 是唯一执行层；`openai.go` / `anthropic.go` / `responses.go` 只做编解码。
- 新增协议 = 新增一个 `*_adapter.go` + 一行路由，不要动 run.go。
