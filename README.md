# MonkeyCode 2API Panel

把 [MonkeyCode](https://monkeycode-ai.com/)（长亭百智云，国内 `.com` / 国际 `.net`）的账号额度，
包装成三套标准协议的反向代理，带账号池面板：

| 协议 | 端点 | 客户端 |
|---|---|---|
| OpenAI Chat Completions | `POST /v1/chat/completions` | 任意 OpenAI SDK / LobeChat / NextChat |
| Anthropic Messages | `POST /v1/messages` | Claude Code、Claude SDK |
| OpenAI Responses（Codex） | `POST /v1/responses` | Codex CLI |

外加 `GET /v1/models`、`GET /status`、`GET /healthz`、面板 `/panel/`。

纯 Go 标准库，零第三方依赖；架构与 [workbuddy2api-panel](https://github.com/misakano7545/workbuddy2api-panel)
同族（账号池 + 冷却 + 面板 + 每日签到），上游协议参考
[monkeycode2api](https://github.com/ZFXing-lite/monkeycode2api)。

---

## ⚠️ 先看这条：上游不是 chat API

MonkeyCode 是 **Agent 任务平台**。一次请求在上游是「建任务 → 挂 WebSocket 任务流 → 消费 ACP 事件」，
VM 里跑的是 `opencode` / `codex` / `claude` 这类 coding agent。因此：

- **延迟高**：一次回答要等沙箱调度 + agent 跑完，通常十几秒起，复杂问题几分钟。
- **一个账号同时只能跑一个任务**（上游业务码 `10811`）。所以并发能力 = 账号数；池里全忙时返回 `503`。
- **没有真正的多轮状态**：每条请求都是一次新任务，历史对话被压成一条 prompt（`[User]` / `[Assistant]` 前缀）。
  system 消息单独走上游的 `system_prompt` 字段。
- **客户端工具表（`tools`）不生效**：上游 agent 有自己的工具循环，建任务接口没有接收客户端工具表的字段。
  本网关不会把 agent 的工具调用下发给客户端（那是 agent 的内部动作，不是客户端要的 function call）。
- **附件不转发**：只取文本部件，图片 / 文件静默丢弃。

把它当「能用额度、能跑通的兼容层」，不要当低延迟 chat API。

---

## 🚀 快速开始

### 1. 配置

```bash
cp config.example.json config.json
export MC2A_API_KEY=$(openssl rand -hex 32)   # 或写进 config.json 的 api_key
```

### 2. 添加账号（粘贴 session）

MonkeyCode 的密码登录要 go-cap 验证码、OAuth 要百智云短信，都自动化不了；唯一稳的路是粘 cookie：

1. 浏览器登录 <https://monkeycode-ai.com/>（国际版 <https://monkeycode-ai.net/>）；
2. F12 → Application → Cookies → 复制 `monkeycode_ai_session` 的值；
3. 本机跑 `./login.sh -region cn`，或直接贴到面板「添加账号」。

```bash
./login.sh -region cn          # 交互粘贴
./login.sh -region intl        # 国际版
```

登录时会顺带做两件事：`GET /api/v1/users/status` 校验，和从历史任务的 `task.image.id`
自动发现 `image_id`（建任务必需）。新账号没有历史任务时发现不到 —— 面板会提示手动填。

### 3. 启动

```bash
go build -o mc2api ./cmd/server && ./mc2api        # 本机
docker compose up -d --build                       # Docker（KEY 走 MC2A_API_KEY）
```

面板：<http://127.0.0.1:7887/panel/>（用同一把 API key 登录）。

### 4. 验证

```bash
curl http://127.0.0.1:7887/healthz
curl -H "Authorization: Bearer $MC2A_API_KEY" http://127.0.0.1:7887/v1/models
./credit.sh        # 额度日报
./signin.sh        # 额度到账检查 + 每日签到
```

---

## 💻 客户端配置

**OpenAI SDK / 通用客户端**

```
Base URL: http://127.0.0.1:7887/v1
API Key:  <你的 api_key>
Model:    见 GET /v1/models（如 openai/gpt-5.4、moonshot/kimi-k2.5）
```

**Claude Code**

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:7887
export ANTHROPIC_AUTH_TOKEN=<你的 api_key>
export ANTHROPIC_MODEL=<GET /v1/models 里的某个 id>
```

**Codex CLI**（`~/.codex/config.toml`）

```toml
model = "<GET /v1/models 里的某个 id>"
model_provider = "monkeycode"

[model_providers.monkeycode]
name = "MonkeyCode"
base_url = "http://127.0.0.1:7887/v1"
env_key = "MC2A_API_KEY"
wire_api = "responses"
```

---

## 🔁 协议映射

```
客户端请求 ──► messages/system ──► 建任务 POST /api/v1/users/tasks
                                    {content, system_prompt?, cli_name, model_id(UUID),
                                     image_id, host_id:"public_host", resource}
                                          │
                                          ▼
                       WS wss://…/api/v1/users/tasks/stream?id=<id>&mode=new
                       连上后先发 auto-approve（工具调用不再等确认）
                       再发 user-input（base64 双层编码，与网页端一致）
                                          │
                          {type, kind, data, timestamp} 帧
                                          │
              task-running kind=acp_event ──► ACP 事件
                                          │
        agent_message_chunk ──► 正文      agent_thought_chunk ──► reasoning
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    ▼                     ▼                     ▼
              OpenAI chunk         Anthropic SSE         Responses 事件
```

| ACP 事件 | OpenAI | Anthropic | Responses |
|---|---|---|---|
| `agent_message_chunk` | `delta.content` | `content_block_delta(text_delta)` | `response.output_text.delta` |
| `agent_thought_chunk` | `delta.reasoning_content` | 丢弃（Anthropic 无对应通道） | 丢弃 |
| `usage_update` / `task-ended.usage` | `usage` 帧 | `message_delta.usage` | `response.completed.usage` |
| `tool_call*` | 不下发（只记日志） | 同 | 同 |

`cli_name` 由平台模型的 `interface_type` 决定：`openai_chat → opencode`、
`openai_responses → codex`、`anthropic → claude`。

---

## ⚙️ 配置

`config.json`（全部项可用 `MC2A_*` 环境变量覆盖）：

| 字段 | 含义 |
|---|---|
| `listen` | 监听地址，默认 `:7887` |
| `api_key` | 网关 + 面板的 API key（`MC2A_API_KEY` 优先，别写进 git） |
| `auth_dir` / `data_dir` | 账号目录 / 状态与日志目录 |
| `cooldown.hard_quota` | 额度耗尽后的长冷却（隔日自动恢复） |
| `cooldown.soft_rate` | 429 / 账号忙（10811）的短冷却 |
| `cooldown.err_threshold` / `err_cooldown` | 连续错误达阈值后的冷却 |
| `schedule.checkin_hours` | 每日签到时刻（已签自动跳过，不重复拿积分） |
| `schedule.quota_check_hour` | 每日额度到账检查时刻 |
| `schedule.quota_refresh_minutes` | 额度刷新间隔 |
| `upstream.idle_timeout_seconds` | 任务流多久没有下行消息算断线（上游每 10s 有 ping） |
| `upstream.task_timeout_seconds` | 单任务总时长上限（与 `resource.life` 对齐） |

环境变量：`MC2A_LISTEN` / `MC2A_API_KEY` / `MC2A_AUTH_DIR` / `MC2A_DATA_DIR` /
`MC2A_SOFT_RATE` / `MC2A_HARD_QUOTA` / `MC2A_ERR_THRESHOLD` / `MC2A_QUOTA_CHECK_HOUR` /
`MC2A_CHECKIN_HOURS`（如 `9,21`）/ `MC2A_CHECKIN_ENABLED` / `MC2A_TASK_TIMEOUT` / `MC2A_IDLE_TIMEOUT`。

---

## 🛠 运维脚本

| 脚本 | 用途 |
|---|---|
| `./login.sh` | 粘贴 session → 校验 → 自动发现 image_id → 落盘 `auths/` |
| `./signin.sh` | 批量「额度到账检查 + 每日签到」（含 Cap.js PoW 验证码自动求解） |
| `./credit.sh` | 钱包 / 额度日报；`-json` 输出原始 JSON |

## 📁 目录

```
cmd/server       HTTP 服务（三套协议 + 面板）
cmd/login        粘贴式登录
cmd/credit       额度日报
cmd/signin       签到 + 到账检查
internal/account 账号（auths/*.json）
internal/upstream 上游：REST / WebSocket / ACP 解析 / 模型目录
internal/pool    账号池（一账号一任务、冷却、停用）
internal/server  协议编解码（openai.go / anthropic.go / responses.go）+ 执行层 run.go
internal/panel   面板（/panel/ + /panel/api/*）
internal/usage   请求日志与每日汇总
internal/scheduler 每日额度检查 + 自动签到
```

## 🔐 隐私

日志只记协议 / 模型 / 账号 id / 状态 / 耗时，不记正文、不记 session。
`auths/`、`data/`、`config.json` 均在 `.gitignore` 内。

## ⚠️ 免责声明

仅供学习与研究。使用者需自行遵守 MonkeyCode / 长亭百智云的服务条款，风险自负。

## License

MIT
