<h1 align="center">MonkeyCode2API Panel</h1>

<p align="center">
  <b>把 MonkeyCode（长亭百智云）账号额度变成 OpenAI / Claude / Codex 三协议 API 的多账号网关 · 附 Web 管理面板</b><br>
  Web 面板 · 账号密码登录 · 官方登录（百智云 / Google / GitHub）· 账号池轮换 · 冷却与熔断 · 每日签到 · 额度到账检查 · 流式 / 非流式
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white&style=flat-square">
  <img alt="API" src="https://img.shields.io/badge/API-OpenAI%20%7C%20Anthropic%20%7C%20Responses-412991?style=flat-square">
  <img alt="Deploy" src="https://img.shields.io/badge/Deploy-Single_Binary%20%7C%20Docker-2496ED?style=flat-square">
  <img alt="Deps" src="https://img.shields.io/badge/Dependencies-0-brightgreen?style=flat-square">
  <img alt="License" src="https://img.shields.io/badge/License-MIT-green?style=flat-square">
</p>

---

> **血统与来路**：本项目的**架构母本**是
> [workbuddy2api-panel](https://github.com/misakano7545/workbuddy2api-panel)（账号池 + 冷却 + 面板 + 定时任务 + 鉴权口径，
> 一比一对齐），上游协议参考 [ZFXing-lite/monkeycode2api](https://github.com/ZFXing-lite/monkeycode2api) 与 agent2api 的
> Rust 实现（`providers/monkeycode`）。**上游协议结论一律实测**：本文写出来的字段名、业务码、帧类型都有对应的真实请求
> 或帧为证；没验过的地方明确标注「未验证」。

> [!IMPORTANT]
> **这条链路的上游已经断了，用之前请知悉。**
>
> 本项目的架构母本 workbuddy2api-panel，其源头 `Sliverkiss/workbuddy2api` **早已删库停更**——原仓库连同代码、issue 与
> 提交历史整体消失，不再可访问。母本自身的直接上游 `linguo2625469/workbuddy2api-panel` 也已于 **2026-10-21 停止维护**
> （不再更新代码、不跟进上游变化、不再处理新的 issue / PR），给出的原因是上游开始禁用反代、作者精力有限，以及
> 批量小号 / 售卖额度的滥用。
>
> 因此本仓**不继承任何官方支持与后续保障**：中间每一环都没有。本仓也不是谁的官方延续，是自用增强分支，按自己的节奏
> 维护——已合入的修复不回退，问题与 PR 在本仓处理。
>
> **维护方式披露**：本仓的 issue / PR 处置、代码审查、测试与修复实现由 Ekko Studio + Hermes Agent（AI 编码 agent）
> 辅助完成，合并与发布决策由维护者本人作出。这与上游停更前一贯的披露方式相同，请知悉后再使用或参与贡献。
>
> **关于作者与维护节奏**：维护者现实身份是一名在读大学生、摄影爱好者，也是玉林市公共汽车公司的公交迷。因家庭经济
> 困难，周末需兼职于玉林市某公司从事相关工作，业余还有自己的兴趣与生活。这不代表有全部精力投入维护——本项目是
> 兴趣驱动，更新随缘、响应不定，可能出现较长时间没有提交；**不要把它当作有 SLA 的商业项目或有求必应的服务**。
> 维护者没有跑路，只是精力有限，看到的问题会在有余力时处理。
>
> 尽管时间有限，仍保有一部分精力用于维护本项目——在自己的电脑上对相关请求做抓包分析（自有设备、本人账号），据此
> 定位并修复接口变化。感谢上游仓库及相关维护者；也感谢长亭科技 MonkeyCode 提供的服务。
>
> **风险自评**：上游随时可能彻底收紧导致通道不可用；因使用非官方客户端导致的账号风控 / 封禁，由使用者自行承担。

> [!WARNING]
> **郑重提醒——这段话针对两类人：现实中与维护者同校的使用者，以及拿本项目搭建对外中转站、做批量小号 / 售卖额度等
> 恶意滥用的人。**
>
> 维护者无法追踪、也不会去追踪任何人的行为，不对任何人行使监控。但必须把话说清楚：上述用法严重违背上游项目作者与
> 维护者的初衷，也是消耗本仓维护动力的最大来源——本项目免费、开源、由一名学生用课余时间维护，**不是给任何人拿去
> 转卖的资源**。本项目是**非官方**网关，仅限**本人授权账号**、本机 / 私有环境自用（详见[安全与合规](#安全与合规)）。
>
> 如果哪一天这个项目确确实实让维护者心力憔悴、不再有动力维护，本仓会像上游那样停止维护，**且不另行通知**。
> 上游的今天，可能就是本仓的明天，请自重。
>
> 同样的底线也适用于同校使用者：若有人出于一己私利，或以维护者无法理解、也不知情的行为，把本项目的情况反映到
> 学校信息中心、维护者所在的二级学院，乃至学院院长与校方其他上级领导，藉此换取奖学金等个人好处，本仓也会同时停止
> 维护。这不是赌气——接受核查、说明与后续处理所占用的时间与心力，已经超出本仓能承受的维护成本。

> [!WARNING]
> **上游不是 chat API——用之前先接受这五条。**
>
> MonkeyCode 是 **Agent 任务平台**：一次请求在上游是「建任务 → 挂 WebSocket → 消费 ACP 事件」，VM 里跑的是
> `opencode` / `codex` / `claude` 这类 coding agent。因此本网关是「额度转换层」，不是低延迟推理接口：
>
> 1. **延迟高**：一次回答要等沙箱调度 + agent 跑完，十几秒起，复杂问题几分钟。
> 2. **一个账号同时只能跑一个任务**（上游 `10811`）。并发能力 = 账号数，池里全忙返回 `503`，**不排队**。
> 3. **没有多轮状态**：每条请求都是新任务，历史被压成一条 prompt；`system` 走上游独立的 `system_prompt` 字段。
> 4. **客户端 `tools` 不生效**：上游 agent 有自己的工具循环，建任务接口没有接收客户端工具表的字段。
>    本网关也**不会**把 agent 的工具调用下发给客户端（那是 agent 内部动作，不是客户端要的 function call）。
> 5. **附件不转发**：只取文本部件，图片 / 文件静默丢弃。

## 项目简介

MonkeyCode2API 是一个自托管的**多协议反向代理网关**，把 MonkeyCode 账号包装成三套标准协议：

| 协议 | 端点 | 客户端 |
|---|---|---|
| OpenAI Chat Completions | `POST /v1/chat/completions` | 任意 OpenAI SDK / LobeChat / NextChat |
| Anthropic Messages | `POST /v1/messages`（也接受 `POST /messages`） | Claude Code、Claude SDK |
| OpenAI Responses（Codex） | `POST /v1/responses` | Codex CLI |

- 官方不提供 OpenAI 形态的开放 API。本项目通过**账号密码登录**（服务端解 Cap.js 验证码换 cookie）或
  **官方登录页 + 粘贴 cookie** 获取账号凭证，在网关侧做账号池调度、冷却与流量治理；
- 面向**个人多账号**场景：多账号共享、单号故障自动换号、冷却防雪崩、忙时换号而不是排队；
- 模型名原样交给上游（`moonshot/kimi-k2.5` 之类），不把 `claude-*` 改写成别的模型；
  同一模型在国内外两站都上架时，按地区各有一条候选，自动挑可用地区。

## 核心能力

| 能力 | 说明 |
|---|---|
| 🔑 **三种登录方式** | 账号密码登录（服务端解 Cap.js PoW 验证码 → `password-login` → 取 `Set-Cookie`，密码不落盘）；官方登录页一键打开（百智云 / Google / GitHub）；粘贴 cookie（DevTools「复制为 cURL」整段也能识别） |
| 🔄 **多账号池** | 按「今日剩余 token 最多 → 加入时间最早」选号；一账号一在途租约，忙就换号（最多换 3 次） |
| 🛡️ **冷却与熔断** | 额度耗尽 `12h` 长冷却、429/账号忙 `90s` 短冷却、连续 5 次错误 `10m` 冷却；登录态失效自动停用该号 |
| 🌍 **双站点** | 国内 `monkeycode-ai.com` / 国际 `monkeycode-ai.net` 同协议、不同 origin，模型目录分地区缓存 |
| ⏰ **定时任务** | 每日额度到账检查（默认 0 点）、额度刷新（30 分钟）、自动签到（09/21 点，已签自动跳过；含 Cap.js PoW 求解） |
| ⚡ **流式 + 非流式** | 三协议各两条路径；流式必给终止帧（`[DONE]` / `message_stop` / `response.completed`），失败进流内错误帧，绝不断连接了事 |
| 🧠 **推理内容** | ACP `agent_thought_chunk` → OpenAI `reasoning_content`；Anthropic / Responses 无对应通道，丢弃（不伪造） |
| 📊 **可观测** | `/status` 汇总 + 每账号明细；面板运行日志按「对话 / 任务 / 系统」三频道分流；每请求一行访问日志（协议 / 模型 / 账号 / 状态 / 耗时，**不记正文**） |
| 🖥️ **Web 管理面板** | go:embed 单二进制内嵌，明暗主题，五个视图：账号池 / 用量 / 模型与档位 / 配置 / 运行日志，见 [Web 管理面板](#-web-管理面板) |
| 📦 **零依赖** | 纯 Go 标准库（含手写 WebSocket 客户端与 Cap.js PoW 求解器），`go.mod` 无 `require` 行；静态二进制约 12 MB |

## 🆚 与参考实现的关系

| 项目 | 关系 |
|---|---|
| [workbuddy2api-panel](https://github.com/misakano7545/workbuddy2api-panel) | **架构母本**。面板三件套（`index.html` + `theme.js` + `app.js` 经 go:embed、内容指纹、严格 CSP）、`internal/httpauth` 统一 Bearer、`{ok,error}` 错误体、账号池/冷却/定时任务的分层，全部照它的形态。**没有照搬**的：积分成长任务、会话粘性、Redis 镜像、券码——MonkeyCode 上游没有对应数据源，硬搬只会造空壳页面 |
| [ZFXing-lite/monkeycode2api](https://github.com/ZFXing-lite/monkeycode2api) | **上游协议参考**。Cap.js 验证码求解、建任务请求体、WS 帧结构、ACP 事件名以它为先验，再逐条打到真实上游复核 |
| agent2api（Rust）`providers/monkeycode` | **错误码与语义参考**。`10811 → 409`、`40100 → 401`、`40002/3/4 → 403`、`50000 → 502` 的映射，以及「`image_id` 从历史任务 `task.image.id` 发现」的策略来自它 |

## 架构总览

```
                    ┌──────────────────────────────────────────────┐
  客户端             │  internal/server                             │
  ├ /v1/chat/…   ──► │  openai.go     纯编解码 ─┐                   │
  ├ /v1/messages ──► │  anthropic.go  纯编解码 ─┼─► run.go 唯一执行层│
  └ /v1/responses──► │  responses.go  纯编解码 ─┘        │          │
                    └───────────────────────────────────┼──────────┘
                                                        ▼
                                        ┌───────────────────────────────┐
                                        │ internal/pool 一账号一租约     │
                                        │ 选号：剩余额度↓ → 加入时间↑    │
                                        │ 冷却 / 停用 / 忙 → 换号（≤3）  │
                                        └───────────────┬───────────────┘
                                                        ▼
                             ┌──────────────────────────────────────────┐
                             │ internal/upstream                        │
                             │  CreateTask  POST /api/v1/users/tasks     │
                             │  Stream      WS …/tasks/stream?id=&mode=  │
                             │  acp.go      {type,kind,data} 帧 → 事件   │
                             └──────────────────────────────────────────┘
```

- `internal/upstream` 只做上游（REST / WebSocket / ACP 解析 / 模型目录），不做协议编解码；
- `internal/server/run.go` 是**唯一**执行层，`openai.go` / `anthropic.go` / `responses.go` 只把 ACP 事件翻译成各协议帧。
  新增协议 = 新增一个编解码文件 + 一行路由，不动 `run.go`；
- `internal/httpauth` 是 `/v1/*` 与 `/panel/api/*` 共用的唯一鉴权口径。

## 快速开始

### 环境要求

- Go **1.26+**（源码构建），或 Docker；
- 一个 MonkeyCode 账号（国内外站均可），**且该账号至少跑过一个任务**——建任务必须带 `image_id`，
  网关从历史任务的 `task.image.id` 自动发现；全新账号发现不到，需要在面板手工填一个镜像 UUID。

### 方式一：Docker Compose（推荐服务器部署）

```bash
# 1. 克隆
git clone https://github.com/misakano7545/monkeycode2api-panel && cd monkeycode2api-panel

# 2. 准备配置（compose 挂载此文件，缺失会导致容器启动失败）
cp config.example.json config.json
#    编辑 config.json 设置 api_key，或导出 MC2A_API_KEY 覆盖

# 3. 启动（首次构建约 1-2 分钟）
export MC2A_API_KEY=$(openssl rand -hex 32)
docker compose up -d --build

# 4. 健康检查
curl -s localhost:7887/healthz
# {"accounts":0,"ok":true}
```

镜像基于 `distroless/static:nonroot`，`CGO_ENABLED=0` 静态编译，版本号经 `--build-arg VERSION=` 注入（面板左下角显示）。

### 方式二：单文件运行（无需 Docker）

```bash
go build -trimpath -ldflags="-s -w" -o mc2api ./cmd/server
./mc2api -config config.json
```

单个静态二进制，交叉编译到 Windows / macOS 均可（`GOOS=windows GOARCH=amd64 go build ./cmd/server`，
本仓**未在 Windows 实机验证**）。浏览器打开面板添加账号：<http://127.0.0.1:7887/panel/>

### 方式三：源码运行（开发调试）

```bash
go run ./cmd/server -config config.json
go vet ./... && go test ./...
```

CI（`.github/workflows/ci.yml`）跑 `gofmt` 检查 + `go vet` + `go test` + 静态构建，**验证一律在 GitHub Actions 上跑**。

### 添加账号（登录）

三种方式，面板「添加账号」里都有。

**方式 A：账号密码登录（全自动，推荐）**

服务端完整模拟官网登录：`POST /api/v1/public/captcha/challenge` → 本地解 Cap.js PoW → `redeem` 换
`captcha_token` → `POST /api/v1/users/password-login` → 取 `Set-Cookie` 的 session →
`GET /api/v1/users/status` 校验 → 自动发现 `image_id` → 落 `auths/`。**密码只在这一次请求里出现，不落盘、不进日志。**

```bash
curl -X POST localhost:7887/panel/api/accounts/login \
  -H "Authorization: Bearer $MC2A_API_KEY" -H 'Content-Type: application/json' \
  -d '{"region":"cn","email":"you@example.com","password":"…"}'
```

限制：只覆盖**账号密码登录**。用百智云 / Google / GitHub 登录的账号本来就没有密码，走方式 B。

**方式 B：官方登录页 + 粘贴**

面板「粘贴 Session」页有 `百智云 / Google / GitHub` 三个按钮：服务端把授权页地址拼好，点开在**你自己的浏览器**里登录，
回来把 cookie 粘上（DevTools 右键请求 →「复制为 cURL」整段也能识别）。

```bash
curl -X POST localhost:7887/panel/api/accounts/oauth/start \
  -H "Authorization: Bearer $MC2A_API_KEY" -H 'Content-Type: application/json' \
  -d '{"region":"intl","method":"github"}'      # method: baizhi | google | github
```

为什么不能像 WorkBuddy 那样点一下就自动进池（零粘贴）——上游结构决定，不是没写：

| 卡点 | 实测证据 |
|---|---|
| `redirect_uri` 写死 | `/users/login?redirect=http://127.0.0.1:7887/x` 的 302 里 `redirect_uri` 仍是 `https://<站点>/api/v1/users/baizhi/callback` |
| 回调由服务端消费 code | `GET /api/v1/users/baizhi/callback?code=fake` → 302 `/login?error=invalid_token`，地址栏不会留下 code |
| 没有设备码 / 轮询端点 | 全量 `/api/v1/*` 里没有对应物；WorkBuddy 有 `/v2/plugin/auth/token?state=…` |
| 国内版没开 Google/GitHub | `GET /users/oauth/github/login` → `10626 该登录方式暂未启用` |
| 国际版百智云没配 | `/users/login` 的 302 里 `client_id=` 为空 |

要真正的零粘贴，只剩「服务器上跑一个浏览器、用户通过面板操作」这条路（chromium + CDP/noVNC），本项目不引入。

**方式 C：粘贴 session（脚本 / 无面板）**

1. 浏览器登录 <https://monkeycode-ai.com/>（国际版 <https://monkeycode-ai.net/>）；
2. F12 → Application → Cookies → 复制 `monkeycode_ai_session` 的值；
3. `./login.sh -region cn`，或贴到面板。

```bash
./login.sh -region cn                 # 交互粘贴
./login.sh -region intl               # 国际版
./login.sh -region cn -image <uuid>   # 手工指定 image_id
```

团队号是另一套 cookie（`monkeycode_ai_team_session`）——粘贴时会被自动识别并记住名字，
但**团队管理员登录**（`/api/v1/teams/users/login`）暂未接入。

> ⚠️ session 是**30 天硬过期、不可续期**。到期只能重新登录；面板账号行会显示到期时间。

### 验证

```bash
curl -s localhost:7887/healthz
curl -s -H "Authorization: Bearer $MC2A_API_KEY" localhost:7887/status | head -c 400
curl -s -H "Authorization: Bearer $MC2A_API_KEY" localhost:7887/v1/models | head -c 400
```

三协议各跑一次（`MODEL` 换成 `/v1/models` 里的 id）：

```bash
# OpenAI chat_completions 流式
curl -N localhost:7887/v1/chat/completions -H "Authorization: Bearer $MC2A_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"$MODEL\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"说一句话\"}]}"

# Anthropic messages（注意 anthropic-version 头）
curl -N localhost:7887/v1/messages -H "x-api-key: $MC2A_API_KEY" -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"$MODEL\",\"max_tokens\":256,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"说一句话\"}]}"

# Codex responses 流式
curl -N localhost:7887/v1/responses -H "Authorization: Bearer $MC2A_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"$MODEL\",\"stream\":true,\"input\":\"说一句话\"}"
```

```bash
./credit.sh        # 额度日报
./signin.sh        # 额度到账检查 + 每日签到
```

## 客户端配置

**OpenAI SDK / 通用客户端**

```
Base URL: http://127.0.0.1:7887/v1
API Key:  <api_key>
Model:    见 GET /v1/models（如 moonshot/kimi-k2.5）
```

**Claude Code**

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:7887
export ANTHROPIC_AUTH_TOKEN=<api_key>
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
| `agent_thought_chunk` | `delta.reasoning_content` | 丢弃（无对应通道） | 丢弃 |
| `usage_update` / `task-ended.usage` | `usage` 帧 | `message_delta.usage` | `response.completed.usage` |
| `tool_call` / `tool_call_update` | 不下发（只记日志） | 同 | 同 |
| `task-error` | 流内错误帧 | `error` 事件 | `response.failed` |
| `task-ended` | `[DONE]` | `message_stop` | `response.completed` |

`cli_name` 由平台模型的 `interface_type` 决定：`openai_chat → opencode`、`openai_responses → codex`、`anthropic → claude`。

> **未验证项（有真实账号后补齐并更新本节）**：`stream.go` 对个别帧 `data` 字段形态的容错（base64 与 JSON 两种写法）、
> `agent_message_chunk` 的文本字段名（`text` vs `content`）、`usage_update` 的完整字段集、
> `image_id` 是否真的每次必填。打开 `MC2A_DEBUG=1` 会把原始帧打到 stderr，用于校准。

## 配置说明

### 字段速查

`config.json`（全部项可用 `MC2A_*` 环境变量覆盖）：

| 字段 | 默认 | 含义 |
|---|---|---|
| `listen` | `:7887` | 监听地址 |
| `api_key` | — | 网关 + 面板的 API key（必填，或走 `MC2A_API_KEY`；别写进 git） |
| `auth_dir` | `./auths` | 账号目录（`auths/*.json`，唯一事实来源） |
| `data_dir` | `./data` | 模型目录缓存、请求日志、每日汇总 |
| `cooldown.hard_quota` | `12h` | 额度耗尽后的长冷却（隔日自动恢复） |
| `cooldown.soft_rate` | `90s` | 429 / 账号忙（`10811`）的短冷却 |
| `cooldown.err_cooldown` | `10m` | 连续错误达阈值后的冷却时长 |
| `cooldown.err_threshold` | `5` | 连续错误多少次触发上面的冷却 |
| `schedule.checkin_enabled` | `true` | 是否自动签到 |
| `schedule.checkin_hours` | `[9,21]` | 每日签到时刻（已签自动跳过，不重复拿积分） |
| `schedule.quota_check_hour` | `0` | 每日额度到账检查时刻 |
| `schedule.quota_refresh_minutes` | `30` | 额度刷新间隔（分钟） |
| `upstream.timeout_seconds` | `30` | 普通 REST 调用超时 |
| `upstream.idle_timeout_seconds` | `300` | 任务流多久无下行消息算断线（上游每 10s 有 ping） |
| `upstream.task_timeout_seconds` | `7200` | 单任务总时长上限（与建任务 `resource.life` 对齐） |
| `upstream.user_agent` | Chrome 131 | 上游请求 UA（过 WAF 需要浏览器 UA + `Origin`/`Referer`） |
| `panel.enabled` | `true` | 是否挂载 `/panel/` |

### 环境变量覆盖

| 变量 | 对应字段 |
|---|---|
| `MC2A_LISTEN` | `listen` |
| `MC2A_API_KEY` | `api_key` |
| `MC2A_AUTH_DIR` / `MC2A_DATA_DIR` | `auth_dir` / `data_dir` |
| `MC2A_SOFT_RATE` / `MC2A_HARD_QUOTA` | `cooldown.soft_rate` / `cooldown.hard_quota` |
| `MC2A_ERR_THRESHOLD` | `cooldown.err_threshold` |
| `MC2A_CHECKIN_ENABLED` | `schedule.checkin_enabled`（`1`/`true`） |
| `MC2A_CHECKIN_HOURS` | `schedule.checkin_hours`（如 `9,21`） |
| `MC2A_QUOTA_CHECK_HOUR` | `schedule.quota_check_hour` |
| `MC2A_TASK_TIMEOUT` / `MC2A_IDLE_TIMEOUT` | `upstream.task_timeout_seconds` / `upstream.idle_timeout_seconds` |
| `MC2A_DEBUG` | `1` 时把上游原始 WS 帧打到 stderr（**只在排查时开，会含正文**） |

## 核心行为语义

### 一账号一任务，忙就换号

上游同一账号并发建第二个任务返回 `10811`。网关的做法是**换号**而不是排队——排队会把延迟拖到几分钟，
对交互式客户端毫无意义。一次请求最多换 3 个号（`maxAttempts = 3`），换不出来返回 `503` + 可读原因
（「账号都在跑任务」/「账号冷却中（额度耗尽，剩 3h）」/「账号今日额度已用尽」）。

客户端主动断开（`ctx.Err() != nil`）**不算账号的错**，不会触发冷却。

### 选号策略

按「今日剩余 token 最多 → 加入时间最早」排序取第一个可用号。剩余额度为 0 的号直接跳过，
冷却中 / 已停用 / 缺 `image_id` 的号跳过并记录原因（`/status` 与 `503` 文案都用这份原因）。

### 错误分类与账号处置

| 上游 | 网关 | 账号动作 |
|---|---|---|
| `40100` / HTTP 401 | `401` | 自动停用（`disabled`） |
| `40002` / `40003` / `40004` | `403` | 不重试 |
| `10811`（账号忙） | `409` / 换号 | 短冷却 |
| `4001` / `4002`（额度） | `429` | 长冷却至隔日 |
| `50000` / 5xx | `502` | 按连续错误计数 |
| 业务码非 0（HTTP 200） | 同上 | **业务码优先于 HTTP 状态** |

### 流式必须显式收尾

流式路径要么给终止帧（`[DONE]` / `message_stop` / `response.completed`），要么给流内错误帧——
绝不「直接断连接」，那在客户端会渲染成「回答被截断」，用户无从判断是失败还是结束。

### 不杜撰 token

上游没给 usage 就报 0，不按字符数估算后写进 `usage`。唯一例外是 Anthropic 的 `count_tokens`——
上游没有对应接口，返回 `chars/3 + 1` 的**粗估**（接口语义要求必须给一个数），不要拿它对账。

## 🔌 API 端点

| 端点 | 鉴权 | 说明 |
|---|---|---|
| `POST /v1/chat/completions` | Bearer | OpenAI 兼容；流式 / 非流式；请求体上限 16 MiB |
| `POST /v1/messages` | Bearer 或 `x-api-key` | Anthropic Messages；流式 / 非流式 |
| `POST /messages` | 同上 | 别名（某些客户端不带 `/v1`） |
| `POST /v1/messages/count_tokens` | 同上 | 粗估 `chars/3+1` |
| `POST /v1/responses` | Bearer | Codex Responses；流式 / 非流式 |
| `GET /v1/models` | Bearer | 上游模型目录（含地区），字段含 `id` / `name` / `region` / `interface_type` |
| `GET /v1/models/{id...}` | Bearer | 单个模型 |
| `GET /status` | Bearer | 汇总（`total` / `healthy` / `cooling` / `disabled`）+ 每账号明细（含 `wallet` / `cooldown` / `expires_at`） |
| `POST /v2/chat/busy` | Bearer | `{busy, free, in_flight}`，给上游 LB 或自建排队用 |
| `GET /healthz` | **无** | `{"ok":true,"accounts":N}`，进程存活即 200（不因无可用账号返回 503） |
| `GET /panel/` | 无（页面不含秘密） | 面板页面 |
| `/panel/api/*` | Bearer | 面板接口，见下 |

鉴权：`api_key` 非空时全部 `/v1/*` 与 `/panel/api/*` 要求 `Authorization: Bearer <api_key>`
（Anthropic 路径额外接受 `x-api-key`，会自动提升成 Bearer 再走同一套校验）。`api_key` 为空 = 不鉴权，仅本机可用。

### 上游端点

| 用途 | 端点 |
|---|---|
| 登录态校验 | `GET /api/v1/users/status` |
| 模型目录 | `GET /api/v1/users/models`、`/api/v1/users/models/available` |
| 钱包 / 签到 | `GET /api/v1/users/wallet`、`GET\|POST /api/v1/users/wallet/checkin` |
| 建任务 | `POST /api/v1/users/tasks` |
| 任务流 | `WS /api/v1/users/tasks/stream?id=<taskId>&mode=new\|attach` |
| 验证码 | `POST /api/v1/public/captcha/challenge`、`/redeem` |
| 账号密码登录 | `POST /api/v1/users/password-login` |
| 官方登录 | `GET /api/v1/users/login`（百智云 302）、`GET /api/v1/users/oauth/{p}/login` |

## 🖥️ Web 管理面板

内嵌式面板（`internal/panel`，前端 `go:embed` 打进二进制，无外部构建依赖），服务启动后访问：

```
http://127.0.0.1:7887/panel/
```

鉴权与 API 同口径：`api_key` 非空时面板要求输入一次密钥（浏览器 `localStorage` 记住）；为空则直接可用。
界面支持**明暗主题切换**（首次跟随系统偏好，点击按钮两态翻转并记住），左侧导航五个视图：

| 视图 | 功能 |
|---|---|
| **账号池** | 统计条（总数 / 可用 / 冷却中 / 已停用 / 今日剩余 token / 积分合计）+ 账号表：状态标签、今日额度进度条、积分、最近签到，单号操作（额度 / 签到 / 停用 / 启用 / 解冷却 / 删除）；批量「刷新额度」「全部签到」；顶部「添加账号」 |
| **添加账号**（顶部按钮） | 三个标签页：**账号密码登录**（服务端解验证码）、**粘贴 Session**（含三个官方登录按钮 + cURL 整段粘贴 + 可选 `image_id`）、**导入 JSON**（单对象或数组，字段与 `auths/*.json` 一致） |
| **用量** | 按天汇总（请求数 / 失败 / 字符 / 输入 token / 输出 token，近 7 / 30 / 90 天）+ 最近请求明细（时间 / 协议 / 模型 / 账号 / 状态 / 耗时 / 错误） |
| **模型与档位** | 上游模型目录：id / 名称 / 地区 / `interface_type` / 上下文 / 输出上限 / 是否免费；「刷新」按地区拉一次上游清单 |
| **配置** | 在线编辑 `config.json` 四组字段（基础 / 冷却 / 排程 / 上游），`api_key` 脱敏回传；保存后返回**需要重启才生效**的字段列表 |
| **运行日志** | 最近 500 行服务日志，按「全部 / 对话 / 任务 / 系统」筛选，可开关自动跟随 |

面板接口挂在 `/panel/api/*`（同一 Bearer 鉴权），可脚本化调用：

```
GET  /panel/api/overview              池总览 + 账号行（前端 5 秒轮询的唯一数据源）
GET  /panel/api/logs                  日志环快照（chat / task / sys 三频道）
GET  /panel/api/models                模型目录（含各地区拉取时间）
POST /panel/api/models/refresh        按地区拉一次上游模型清单
GET  /panel/api/usage?days=7          按天用量
GET  /panel/api/request_logs?limit=60 最近请求
GET  /panel/api/config                当前配置（api_key 脱敏）
POST /panel/api/config                保存配置（返回需重启的字段）
POST /panel/api/accounts/login        账号密码登录
POST /panel/api/accounts/oauth/start  官方登录授权页地址
POST /panel/api/accounts/add_session  粘贴 session 添加（校验 + 发现 image_id）
POST /panel/api/accounts/import       批量导入账号 JSON
POST /panel/api/accounts/{id}/checkin|balance|disable|revive|cooldown_clear|remove
POST /panel/api/checkin_all           全部签到
POST /panel/api/balance_all           全部刷新额度
```

错误体统一 `{"ok":false,"error":"…"}`。

**安全响应头**：面板页面与全部 `/panel/api/*` 统一带 `Content-Security-Policy`
（`default-src 'none'`，脚本仅同源 `'self'`，`frame-ancestors 'none'`）、`X-Content-Type-Options: nosniff`、
`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`、COOP/CORP，并 `Cache-Control: no-store`。
前端脚本是独立的同源 `app.js` / `theme.js`，**不含内联脚本与内联事件处理器**（事件委托），脚本 URL 带内容指纹。

**鉴权实现**：`internal/httpauth` 统一 server 与 panel 的 Bearer 校验，SHA-256 摘要 +
`subtle.ConstantTimeCompare` 常量时间比较；`key` 为空表示不鉴权。

> ⚠️ 公网部署：服务自身只提供明文 HTTP，**务必置于 HTTPS 反向代理之后**（Nginx / Caddy）并加访问限流。

## 请求级日志

每请求一行（stdout，同时进面板日志环）：

```
2026/10/23 09:45:04 POST /v1/chat/completions 200 model=moonshot/kimi-k2.5 acct=cn-3f2a 18.4s 512→1024
```

- **只记**协议、模型、账号 id、状态、耗时、字符 / token 数；
- **不记**正文、不记 session、不记 cookie；
- `data/requests.jsonl` 存结构化记录（面板「用量」读它），每日汇总另存。

## 部署运维

### Docker 镜像

```bash
docker compose build
docker compose up -d
docker compose logs -f
```

挂载三处：`./auths`（账号，**必须持久化**）、`./data`（缓存与日志）、`./config.json`（只读）。
容器以 `nonroot` 运行，宿主目录属主需对容器 uid 可写，否则登录落盘会 `permission denied`。

### 工具脚本

| 脚本 | 用途 |
|---|---|
| `./login.sh` | 粘贴 session → 校验 → 自动发现 `image_id` → 落盘 `auths/` |
| `./signin.sh` | 批量「额度到账检查 + 每日签到」（含 Cap.js PoW 自动求解） |
| `./credit.sh` | 钱包 / 额度日报；`-json` 输出原始 JSON |

### 账号管理

- 账号文件是 `auths/<region>-<user_id>.json`，**唯一事实来源**；直接删文件等价于移除账号；
- 面板「停用」只改本地状态，不影响上游；
- session 30 天硬过期、**不可续期**，到期需重新登录；
- 批量迁移：面板「导入 JSON」，或直接把 `auths/*.json` 拷过去。

### 目录

```
cmd/server         HTTP 服务（三套协议 + 面板）
cmd/login          粘贴式登录
cmd/credit         额度日报
cmd/signin         签到 + 到账检查
internal/account   账号（auths/*.json）、cookie 名归一化
internal/upstream  上游：REST / WebSocket / ACP 解析 / 模型目录 / 验证码 / 登录
internal/pool      账号池（一账号一任务、冷却、停用）
internal/server    协议编解码（openai / anthropic / responses）+ 执行层 run.go
internal/panel     面板（index.html + theme.js + app.js 经 go:embed；/panel/api/* 分域）
internal/httpauth  Bearer 鉴权（server 与 panel 同一口径）
internal/usage     请求日志与每日汇总
internal/scheduler 每日额度检查 + 自动签到
```

## 安全与合规

### 本仓定位

- 自用工具，**非官方客户端**，不提供任何形式的托管服务；
- 仅限**本人授权账号**、本机或私有网络使用；
- 不得用于搭建对外中转站、批量注册小号、售卖额度。

### 1. 凭据管理

`auths/*.json` 内含 session cookie，等同于账号密码。`auths/`、`data/`、`config.json`、`.session` 全部在
`.gitignore` 内；提交前会做一次密钥扫描。

### 2. 网络暴露与日志

- 面板与 API 共用一把 key，公网必须上 HTTPS 反代；
- 日志不落正文与凭据，因此**不要把 `MC2A_DEBUG=1` 长期开着**——它会打印上游原始帧（含回答正文）。

### 3. 上游风控

上游有 WAF：缺 `Origin` / `Referer` / 浏览器 UA 的请求会直接 `403 禁止访问`。本项目按浏览器形态发请求，
但这不等于不会被风控；账号因非官方使用被限制的风险由使用者承担。

## 常见问题

### 加账号时报「登录态校验失败：上游登录态失效（HTTP 401）」

session 无效或站点选错。国内版 `.com`、国际版 `.net` 的 cookie 不通用；也确认 cookie 没被截断
（面板支持整段 cURL 粘贴，比手抄可靠）。

### 账号加进去了，但 `/status` 里 `healthy: 0`，请求返回 503

大概率缺 `image_id`。`/status` 里该字段是布尔（有没有）。去官网跑一个任务让网关下次自动发现，
或手工填一个镜像 UUID。

### 一直返回「账号都在跑任务」

账号数少于并发数。上游硬限制一账号一任务，**只能加号**——排队在本项目里不存在。

### 签到没加上积分

先看 `./signin.sh` 的输出：已签会显示跳过（不重复给）。签到需要过 Cap.js 验证码，求解失败会明确报错
（`子挑战 N 在 … 次内未解出`），重跑一次通常就好。

### 流式回答到一半停了

看服务日志里的 `task-error` 帧与 `idle_timeout`。上游 agent 崩溃 / 沙箱超时都会表现为流内错误帧，
客户端应显示为错误而不是截断。若日志里连 `task-ended` 都没有，说明是网络中断——`idle_timeout_seconds`
默认 300s，可调小以更快失败。

### `count_tokens` 的数字和真实用量差很多

这是**粗估**（`chars/3+1`），上游没有对应接口。要真实用量看响应里的 `usage`。

## 关键断言 ↔ 代码出处

| 本文断言 | 出处 |
|---|---|
| 一账号一任务、忙则换号（≤3 次） | `internal/server/run.go`（`maxAttempts`）、`internal/pool/pool.go`（`Acquire` 的 `busy` 判断） |
| 选号：剩余额度↓ → 加入时间↑ | `internal/pool/pool.go`（`sort.SliceStable`） |
| `10811` → 忙，`40100` → 停用 | `internal/upstream/client.go`（`classifyCode`） |
| `system` 单独走 `system_prompt` | `internal/upstream/task.go`（`BuildPrompt`） |
| `cli_name` 由 `interface_type` 决定 | `internal/upstream/region.go`（`CliNameFor`） |
| 流式必给终止帧 | `internal/server/openai.go` / `anthropic.go` / `responses.go` |
| 不杜撰 usage | `internal/server/run.go`（只在 `EvUsage` 时写） |
| 面板 CSP / 指纹 / 三件套 embed | `internal/panel/index.go` |
| Bearer 常量时间比较 | `internal/httpauth/httpauth.go` |
| Cap.js PoW 求解 | `internal/upstream/captcha.go` |
| 密码登录链路 | `internal/upstream/client.go`（`PasswordLogin`） |
| OAuth 只能做前半段 | `internal/upstream/oauth.go`（文件头注释，含实测证据） |
| session 30 天硬过期 | `internal/account/account.go`（`SessionTTL`） |

## 免责声明

仅供学习与研究。使用者需自行遵守 MonkeyCode / 长亭百智云的服务条款，账号风控与封禁风险自负。
本项目不收集、不上传任何账号数据；所有凭据只存在你本机的 `auths/`。

## License

MIT
