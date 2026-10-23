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

### 2. 添加账号

**方式 A：账号密码登录（面板里点「添加账号」→ 账号密码登录）**

服务端完整模拟官网登录：`POST /api/v1/public/captcha/challenge` → 本地解 Cap.js PoW →
`redeem` 换 `captcha_token` → `POST /api/v1/users/password-login` → 取 `Set-Cookie` 的 session →
`GET /api/v1/users/status` 校验 → 自动发现 `image_id` → 落 `auths/`。密码只在这一次请求里出现，
不落盘、不进日志。

```bash
curl -X POST localhost:7887/panel/api/accounts/login \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"region":"cn","email":"you@example.com","password":"…"}'
```

限制：只覆盖**账号密码登录**。用百智云 OAuth / Google / GitHub 登录的账号本来就没有密码
（`GET /api/v1/users/login` 是 302 到 `baizhi.cloud/oauth/authorize`，短信验证码自动化不了），
团队管理员走的是 `/api/v1/teams/users/login` 另一套 cookie —— 这两种仍用方式 B。

**方式 B：官方登录 + 粘贴（OAuth 只能做到这一步）**

面板「粘贴 Session」页有 `百智云 / Google / GitHub` 三个按钮：服务端把授权页地址拼好，
点开在**你自己的浏览器**里登录，回来把 cookie 粘上（DevTools 里右键请求 →「复制为 cURL」整段也能识别）。

```bash
curl -X POST localhost:7887/panel/api/accounts/oauth/start \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"region":"intl","method":"github"}'      # method: baizhi | google | github
```

为什么不能像 WorkBuddy 那样点一下就自动进池（零粘贴）：

| 卡点 | 实测证据 |
|---|---|
| `redirect_uri` 写死 | `/users/login?redirect=http://127.0.0.1:7887/x` 的 302 里 `redirect_uri` 仍是 `https://<站点>/api/v1/users/baizhi/callback` |
| 回调由服务端消费 code | `GET /api/v1/users/baizhi/callback?code=fake` → 302 `/login?error=invalid_token`，地址栏不会留下 code |
| 没有设备码 / 轮询端点 | 全量 `/api/v1/*` 里没有对应物；WorkBuddy 有 `/v2/plugin/auth/token?state=…` |
| 国内版没开 Google/GitHub | `GET /users/oauth/github/login` → `10626 该登录方式暂未启用` |
| 国际版百智云没配 | `/users/login` 的 302 里 `client_id=` 为空 |

要真正的零粘贴，只剩「服务器上跑一个浏览器、用户通过面板操作」这条路（chromium + CDP/noVNC），
本项目不引入。

**方式 C：粘贴 session**

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

面板结构与本族其他 `*2api-panel` 一致（照 `workbuddy2api-panel` 的形态）：

| 前端 | 后端 |
|---|---|
| `internal/panel/index.html` 页面骨架 + 设计令牌（明暗两套） | `internal/panel/panel.go` 路由、`withAuth`、`writeJSON`/`writeErr` |
| `internal/panel/theme.js` 首屏主题预热（`<head>` 同步加载） | `internal/panel/index.go` go:embed + 内容指纹 + 严格 CSP |
| `internal/panel/app.js` 全部前端逻辑（无内联脚本，CSP 免 unsafe-inline） | `internal/panel/{accounts,models,usage,config,ring}.go` 分域处理 |

页面：**账号池**（统计条 + 账号表 + 单号操作）、**用量**（按天 + 最近请求）、
**模型与档位**、**配置**（读写 `config.json`，密钥脱敏回传）、**运行日志**（按频道筛选）。

接口（`Authorization: Bearer <api_key>`，与 `/v1/*` 同一口径）：

```
GET  /panel/api/overview             池总览 + 账号行（5 秒轮询的唯一数据源）
GET  /panel/api/logs                 日志环快照（chat / task / sys 三频道）
GET  /panel/api/models               模型目录（含各地区拉取时间）
POST /panel/api/models/refresh       按地区拉一次上游模型清单
GET  /panel/api/usage?days=7         按天用量
GET  /panel/api/request_logs?limit=60 最近请求
GET  /panel/api/config               当前配置（api_key 脱敏）
POST /panel/api/config               保存配置（返回需重启的字段）
POST /panel/api/accounts/add_session 粘贴 session 添加（校验 + 发现 image_id）
POST /panel/api/accounts/login      账号密码登录（服务端解验证码换 cookie）
POST /panel/api/accounts/oauth/start 官方登录授权页地址（百智云/Google/GitHub）
POST /panel/api/accounts/import      批量导入账号 JSON
POST /panel/api/accounts/{id}/checkin|balance|disable|revive|cooldown_clear|remove
POST /panel/api/checkin_all          全部签到
POST /panel/api/balance_all          全部刷新额度
```

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
internal/panel   面板（index.html + theme.js + app.js 经 go:embed；/panel/api/* 分域处理）
internal/httpauth Bearer 鉴权（server 与 panel 同一口径）
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
