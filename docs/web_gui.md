# Aqua Web GUI 设计文档（评审修订版）

> 基于当前 `quailyquaily/aqua` CLI 与存储/运行时语义整理的 Web Console 设计稿。本文档描述的是目标设计，其中 Web Console 相关 flag、HTTP API、WebSocket 与前端静态资源目录挂载均为拟新增能力，并非当前仓库已实现能力。

- 文档状态：proposal
- 文档版本：v2.1-review
- 评审时间：2026-03-11
- 评审基线：仓库当前 `cmd/aqua/*`、`aqua/*`、`docs/cli.md`

---

## 0. 评审结论

这份设计稿的方向成立，适合作为 Aqua Web GUI 的 roadmap，但有几处需要在正式文档里修正，否则会把尚未实现或语义不准确的内容写成既成事实。

需要明确的修订点：

- 当前仓库没有 `aqua serve --web-console`、`--web-console-addr`、`--web-console-token`、`--console-web-dst-dir`，也没有内置 HTTP/REST/WebSocket 控制台。
- 当前 `aqua serve` 是前台长运行的 libp2p 节点进程。若未来 Web Console 直接内嵌在这个进程中，GUI 不能在 v1 中提供“停止当前进程”的按钮，否则会把自己一起停掉。
- 当前“实时能力”来自两类现有语义：`aqua serve --json` 输出事件，以及 `aqua inbox watch` 对本地 inbox 的轮询等待。WebSocket 推送是合理目标，但仍需新增实现。
- 当前 `contacts add <address>` 是联网操作：它会拨号到目标地址拉取 signed contact card，然后导入本地；这不是纯本地写入。
- 当前群消息不是单独的原生消息模型，而是以 `group.message.v1` topic 进入 inbox/outbox；payload 是 JSON envelope，前端需要解析后再聚合成群会话。
- 当前 `group send` 没有 `reply_to`/`session_id` 语义；因此“回复消息”在 v1 只适用于点对点消息，不适用于群消息。
- 当前 `aqua card export` 直接输出 signed contact card JSON，本身不是 `--json` 包装接口。

据此，本文档保留原始目标，但将以下事项调整为 v1 约束：

- v1 不提供 GUI 内的 `serve start/stop`。
- v1 的 `Settings` 以只读运行时/启动参数展示为主，不引入新的配置文件语义。
- v1 的 Relay 管理仅展示当前节点的 relay 连接/地址信息，不尝试接管单独的 `aqua relay serve` 运维面板。

---

## 1. 目标与范围

### 1.1 设计目标

- 将 Aqua 现有 CLI 能力映射到 Web GUI，不引入 CLI 目前没有的业务语义。
- 面向两类用户：
  - AI Agent 操作员：管理本地节点、联系人、消息与群组状态。
  - 高级个人用户：桌面环境下的日常消息、联系人与群组使用。
- 消息视图需要 live update，最终形态以 WebSocket 推送为目标，不依赖手工刷新。
- 前端统一使用 Vue 3 + TypeScript + `quail-ui` morph theme，以 Vue SPA 形态实现，不引入额外 UI 库。
- 先保证语义正确，再逐步优化体验与视觉细节。

### 1.2 v1 不在范围内

- 端到端密钥可视化管理。
- 独立 Relay 节点的完整运维面板。
- 移动端适配。
- 多节点统一控制台。
- 通过 GUI 启停当前 `aqua serve` 进程。

---

## 2. 当前实现基线

这一节不是目标方案，而是当前仓库已经存在的能力边界，后续 Web GUI 设计必须以它为准。

### 2.1 当前 CLI 已有能力

| 能力 | 当前 CLI |
| --- | --- |
| 身份 | `aqua init`、`aqua id [nickname]` |
| Contact Card | `aqua card export` |
| 联系人 | `aqua contacts list/add/import/show/verify/del` |
| 点对点消息 | `aqua send`、`aqua inbox list/watch/mark-read`、`aqua outbox list` |
| 群组 | `aqua group create/list/show/invite/invites/invite accept/invite reject/send` |
| 节点运行 | `aqua serve` |
| Relay 服务 | `aqua relay serve`、`aqua relay peers` |

### 2.2 当前语义要点

- `aqua send` 默认 topic 为 `chat.message`，默认 `content-type` 为 `text/plain`。
- 直接消息支持 `reply_to` 与自动生成的 `session_id`。
- inbox/outbox 都以本地文件存储为准，消息列表是本地查询结果。
- `aqua inbox watch` 本质是轮询本地 inbox，默认 `250ms` 轮询间隔。
- `aqua serve` 在收到消息时会写入 inbox，并可通过 stdout/json 输出 `agent.data.push` 事件。
- Relay 事件已有内部结构体 `RelayEvent`，例如 `relay.path.selected`、`relay.fallback`、`relay.reservation.ok`。
- 群控制消息使用 topic `group.control.v1`。
- 群普通消息使用 topic `group.message.v1`，payload 是 JSON envelope：
  - `version`
  - `group_id`
  - `epoch`
  - `sender_peer_id`
  - `content_type`
  - `message`
- `group send` 是 sender-side fanout，逐个成员发送，不是中心化群服务器。

### 2.3 当前尚未实现的 Web Console 能力

- `serve` 子命令上的 Web Console 相关 flag。
- 嵌入式 HTTP API。
- WebSocket 事件广播层。
- 前端静态资源目录挂载。
- Bearer Token 鉴权。
- 可写配置模型。
- 进程 supervisor。

---

## 3. 架构概述

### 3.1 目标架构

```text
Browser (Web GUI)
      |  HTTP REST + WebSocket
      v
aqua serve --web-console --console-web-dst-dir <dir>   # 拟新增
  |- Web Console API Layer           # 拟新增
  |    |- REST: node / contacts / inbox / outbox / groups
  |    \- WS: agent.data.push / relay.* / read-state updates
  |- Serve built Vue SPA from dir    # 拟新增
  \- aqua core
       \- ~/.aqua
```

### 3.2 v1 架构约束

- Web Console 作为 `aqua serve` 的附加能力，而不是独立的中间层服务。
- 前端以 Vue SPA 单独构建；传入 `--console-web-dst-dir` 时由 `aqua serve` 提供静态文件，不传时仅暴露 API / WebSocket。
- 因为没有 supervisor，v1 中 GUI 只在 `aqua serve --web-console` 已经启动时可访问。
- 因为 GUI 与节点在同一进程内，v1 不提供“停止当前进程”的操作。
- 前端只调用 Web Console API，不通过浏览器直接调用 CLI，也不依赖 shell bridge。
- 首先支持 localhost 场景，远程访问与鉴权放到后续阶段。

### 3.3 未来计划新增的启动方式

```bash
# 计划中的本地模式
aqua serve --web-console --console-web-dst-dir ./web/dist

# 计划中的显式监听地址
aqua serve --web-console --web-console-addr 127.0.0.1:8089 --console-web-dst-dir ./web/dist

# 计划中的远程鉴权
aqua serve --web-console --web-console-token <token> --console-web-dst-dir ./web/dist
```

其中 `--console-web-dst-dir` 指向 Vue SPA 的构建输出目录；该参数可省略，省略时只启动 API / WebSocket，不托管静态前端。

---

## 4. 前端技术选型

| 项目 | 选型 |
| --- | --- |
| 框架 | Vue 3 + TypeScript |
| UI 库 | `quail-ui`（固定 `morph` theme） |
| 状态管理 | Pinia |
| 实时通信 | WebSocket |
| 样式 | `quail-ui` CSS custom properties + SCSS |
| 构建 | Vite |
| 交付形态 | Vue SPA 构建产物目录，由 `aqua serve` 挂载 |

初始化方式：

```ts
import { createApp } from 'vue'
import { QuailUI, applyTheme } from 'quail-ui'
import 'quail-ui/dist/index.css'

applyTheme('morph')

const app = createApp(App)
app.use(QuailUI)
app.mount('#app')
```

构建要求：

- 前端项目以 SPA 方式产出静态文件。
- 构建产物输出到指定目录，例如 `./web/dist`。
- `aqua serve --console-web-dst-dir <dir>` 在传入时把该目录作为 Web Console 的静态资源根目录；不传时仅提供 API / WebSocket。
- v1 不要求 `go:embed`；后续若需要单文件分发，可再评估嵌入式打包。

---

## 5. 信息架构

### 5.1 导航结构

桌面布局：左侧栏 + 右侧主内容区。

```text
Aqua Web GUI
├── Messages
├── Contacts
├── Node
└── Settings
```

`Compose` 不做独立路由，在 `Messages` 内触发。

### 5.2 全局状态栏

顶部固定状态栏展示：

- 节点运行模式：`Web Console attached`
- Relay 状态：Connected / Fallback / Disconnected
- 未读消息数
- 当前节点昵称 / peer_id 简写

说明：

- 若采用内嵌式 `serve --web-console`，用户能打开 GUI 时说明进程已经存在，因此不展示“请先启动当前进程”的主流程提示。
- 若后续引入 supervisor，再补充 `Running / Stopped` 的进程级状态。

---

## 6. 页面设计

### 6.1 Messages 页 `/messages`

#### 6.1.1 设计原则

- 采用类 IM 双栏布局。
- 点对点消息与群消息统一在一个主视图中，但前端必须承认它们来自不同底层语义。
- 会话是前端聚合模型，不要求后端直接返回现成 `Conversation`。

#### 6.1.2 会话来源

点对点会话：

- 从 inbox/outbox 中选取非群 topic 的消息记录。
- 以对端 `peer_id` 聚合。

群会话：

- 群元数据来自 `group list` / `group show`。
- 群消息来自 `topic = group.message.v1` 的 inbox/outbox 记录。
- 前端需要解析 payload envelope 提取：
  - `group_id`
  - `sender_peer_id`
  - 逻辑 `content_type`
  - 逻辑消息文本

待处理邀请：

- 来自 `group invites --incoming --status pending` 对应的 API。
- 在会话列表顶部作为单独分区呈现，而不是伪装成普通消息会话。

#### 6.1.3 会话列表字段

| 字段 | 说明 |
| --- | --- |
| 头像 | `display_name` 或 `nickname` 首字母，使用 `QAvatar` |
| 名称 | `display_name > nickname > peer_id 截断` |
| 最后消息摘要 | 文本前 60 字符；群消息展示解析后的逻辑文本 |
| 时间 | 相对时间 |
| 未读 badge | 基于 inbox read state 聚合 |
| 类型标识 | 群会话显示群图标 |

排序：按会话最后更新时间倒序。

过滤：按名称、`peer_id`、`group_id` 过滤。

#### 6.1.4 消息内容区

直接消息：

- 自己发出：右对齐。
- 收到消息：左对齐。
- hover 显示完整时间、message id、topic、content type。
- 支持“标记已读”“复制 message id”“回复”。

群消息：

- 顶部显示 `group_id`、`my_role`、成员数。
- 每条消息展示 `sender_peer_id` 对应的显示名。
- hover 显示原始 message id 与 envelope 元数据。
- v1 不提供“回复消息”动作，因为当前 `group send` 不支持 `reply_to`。

#### 6.1.5 输入区

直接消息输入：

| 字段 | 说明 |
| --- | --- |
| Message | `QTextarea`，Enter 发送，Shift+Enter 换行 |
| Topic | 折叠高级选项，默认 `chat.message` |
| Content-Type | 折叠高级选项，默认 `text/plain` |
| Reply-To | 仅当从消息 hover 菜单触发时预填 |
| Send | `QButton` primary |

群消息输入：

| 字段 | 说明 |
| --- | --- |
| Message | `QTextarea` |
| Logical Content-Type | 默认 `text/plain`，写入 group envelope 内部字段 |
| Send | 调用群消息发送 API |

群消息发送时，前端不允许用户直接编辑外层 topic；外层固定为 `group.message.v1`。

#### 6.1.6 新建会话

点击“+ 新建会话”打开 `QDialog`：

- 输入 `peer_id`
- 或从联系人列表选择
- 可选输入 override address

说明：

- 仅输入 `peer_id` 时，发送依赖本地联系人中已有地址。
- 若没有联系人记录，直接消息发送会失败；此时应引导用户先添加联系人。

### 6.2 Contacts 页 `/contacts`

布局：列表 + 详情面板。

列表字段：

| 字段 | 说明 |
| --- | --- |
| 头像 | `QAvatar` |
| Display Name | 本地别名 |
| Nickname | 对方上报昵称 |
| Peer ID | 截断显示，hover 展开 |
| Trust State | `QFence` 或等效 badge |

Trust State 颜色：

- `verified` -> success
- `tofu` -> warning
- `conflicted` / `revoked` -> danger

操作：

- 添加联系人：
  - 主路径：输入 multiaddr，调用“fetch & import”接口。
  - 该操作需要目标 peer 在线可达。
- 导入联系人卡片：
  - 粘贴或上传 contact card JSON。
  - 映射 `contacts import` 语义。
- 删除联系人：
  - 仅删除本地联系人记录，不等于撤销远端身份。
- 标记已验证：
  - 仅在带外确认完成后执行。
- 发送消息：
  - 跳转到对应 direct conversation。

### 6.3 Node 页 `/node`

展示内容：

| 字段 | 来源语义 |
| --- | --- |
| Peer ID | `aqua id --json` |
| Nickname | `aqua id --json` |
| Listen Addresses | `serve` 运行时 |
| Advertised Addresses | 节点当前地址集 |
| Relay 状态 | relay 事件流 |
| Relay Circuit 地址 | 当前可用 relay advertise 地址 |
| 最近 relay 事件 | `RelayEvent` 序列 |

操作：

- 修改 Nickname
- 导出 / 下载 / 复制 Contact Card
- 复制当前启动命令模板

v1 不提供：

- 启动当前 `serve`
- 停止当前 `serve`
- 控制独立 `aqua relay serve`

### 6.4 Settings 页 `/settings`

v1 定位：只读运行时与启动参数信息页，而不是完整配置中心。

展示内容：

- `AQUA_DIR`
- relay mode
- configured relay endpoints
- log level
- Web Console bind address
- console web dst dir
- 复制重启命令模板

后续阶段再考虑：

- 可写配置文件
- GUI 内修改并持久化默认 relay
- GUI 内修改 log level
- GUI 内修改启动参数并热重启

---

## 7. 关键交互设计

### 7.1 Peer ID 显示规则

全局统一：

- 优先 `display_name`
- 其次 `nickname`
- 最后 `peer_id` 截断为 `前 8 位 + ... + 后 4 位`

完整值始终支持 hover 展开和复制。

### 7.2 实时更新

目标形态：

```text
libp2p inbound message / relay event
        |
        v
Web Console event broadcaster
        |
        v
WebSocket client
        |
        +-- 插入消息流
        +-- 更新会话排序
        +-- 更新未读数
        \-- 更新 relay 状态栏
```

设计要求：

- 前端不依赖手工刷新。
- 事件格式尽量复用现有 `DataPushEvent` / `RelayEvent` 字段命名。
- 前端在连接恢复后应回退到一次 REST re-sync，避免漏事件。

### 7.3 错误处理

API 错误统一展示：

1. 错误代码
2. 人话解释
3. 建议动作

使用 `QFence` 的 `warning` 或 `danger` 风格。

典型错误提示：

- 联系人不存在
- 联系人未验证或处于 conflicted/revoked
- peer 离线或地址不可达
- relay fallback 后仍连接失败
- 群邀请已过期
- 群发送部分成员失败

---

## 8. Web Console API 设计

统一前缀：`/api/console/v1`

说明：

- 下表是拟新增 API，不表示当前仓库已经存在这些 HTTP 端点。
- API 尽量贴近现有 CLI 与 service/store 语义，但不强求 URL 结构与 CLI 完全一一对应。

### 8.1 REST 端点

| Method | Path | 语义 | 对应 CLI/内部能力 |
| --- | --- | --- | --- |
| GET | `/node/id` | 获取本地身份 | `aqua id --json` |
| PUT | `/node/id` | 更新 nickname | `aqua id <nickname>` |
| GET | `/node/runtime` | 获取当前运行时信息 | `serve` 运行态 + relay 事件状态 |
| GET | `/node/card` | 返回 signed contact card JSON | `aqua card export` |
| GET | `/contacts` | 联系人列表 | `aqua contacts list --json` |
| POST | `/contacts/fetch` | 通过 address 拉取并导入 contact card | `aqua contacts add` |
| POST | `/contacts/import` | 导入已给定的 contact card JSON | `aqua contacts import` |
| GET | `/contacts/:peer_id` | 联系人详情 | `aqua contacts show --json` |
| DELETE | `/contacts/:peer_id` | 删除联系人 | `aqua contacts del` |
| POST | `/contacts/:peer_id/verify` | 标记已验证 | `aqua contacts verify` |
| GET | `/inbox` | inbox 列表 | `aqua inbox list --json` |
| POST | `/inbox/mark-read` | 标记已读 | `aqua inbox mark-read` |
| GET | `/outbox` | outbox 列表 | `aqua outbox list --json` |
| POST | `/send/direct` | 发送点对点消息 | `aqua send` |
| GET | `/groups` | 群组列表 | `aqua group list --json` |
| POST | `/groups` | 创建群组 | `aqua group create` |
| GET | `/groups/:group_id` | 群详情 | `aqua group show --json` |
| GET | `/groups/invites` | 群邀请列表 | `aqua group invites --json` |
| POST | `/groups/:group_id/invites` | 创建邀请 | `aqua group invite` |
| POST | `/groups/:group_id/invites/:invite_id/accept` | 接受邀请 | `aqua group invite accept` |
| POST | `/groups/:group_id/invites/:invite_id/reject` | 拒绝邀请 | `aqua group invite reject` |
| POST | `/groups/:group_id/messages` | 发送群消息 | `aqua group send` |

### 8.2 请求/响应语义约束

`GET /node/card`

- 返回值应与 `aqua card export` 的 signed contact card JSON 一致。
- 不额外包一层 `{"data": ...}` 不是必须，但全局 API 若采用统一 envelope，也应保留原始 card 结构完整性。

`POST /contacts/fetch`

- 请求体至少包含：
  - `address`
  - `display_name?`
  - `verify?`
- 该接口是联网操作，目标 peer 不在线时会失败。

`POST /send/direct`

- 请求体建议包含：
  - `peer_id`
  - `message`
  - `topic?`
  - `content_type?`
  - `reply_to?`
  - `session_id?`
  - `idempotency_key?`
- 默认值应与 CLI 一致：
  - `topic = chat.message`
  - `content_type = text/plain`
  - 未显式传入时自动生成 `session_id`

`POST /groups/:group_id/messages`

- 请求体建议包含：
  - `message`
  - `content_type?`
- 服务端语义必须与当前 `group send` 保持一致：
  - 外层 topic 固定 `group.message.v1`
  - 外层 `content_type` 固定 `application/json`
  - 逻辑 `content_type` 放在 envelope 内部
  - 返回 `attempted_peers` / `sent_peers` / `failed_peers`

### 8.3 WebSocket 端点

建议使用单一多路复用事件流：

| Path | 说明 |
| --- | --- |
| `ws://host/api/console/v1/ws/events` | 统一推送消息、relay、read-state 事件 |

原因：

- 当前 `serve` 已经天然产生两类核心事件：`DataPushEvent` 与 `RelayEvent`。
- 单连接更适合前端初始化、断线重连与状态同步。

### 8.4 WebSocket 事件格式

消息事件建议直接贴近当前 `agent.data.push` 输出：

```json
{
  "event": "agent.data.push",
  "from_peer_id": "12D3KooW...",
  "topic": "chat.message",
  "content_type": "text/plain",
  "idempotency_key": "msg_...",
  "session_id": "0195...",
  "reply_to": "",
  "received_at": "2026-03-10T06:31:53Z",
  "payload_text": "hello",
  "payload_base64": "aGVsbG8"
}
```

Relay 事件建议沿用当前 `RelayEvent`：

```json
{
  "event": "relay.path.selected",
  "path": "relay",
  "relay_peer_id": "12D3KooWRelay...",
  "target_peer_id": "12D3KooWPeer...",
  "reason": "",
  "timestamp": "2026-03-10T06:31:53Z"
}
```

read-state 更新可使用控制台自定义事件：

```json
{
  "event": "inbox.read_state.changed",
  "message_ids": ["msg_1", "msg_2"],
  "marked": 2,
  "timestamp": "2026-03-10T06:32:10Z"
}
```

---

## 9. 前端聚合数据模型

### 9.1 Conversation

```ts
interface Conversation {
  id: string
  kind: 'direct' | 'group'
  title: string
  peer_id?: string
  group_id?: string
  last_message_preview?: string
  unread_count: number
  updated_at: string
}
```

### 9.2 Contact

```ts
interface Contact {
  peer_id: string
  node_uuid: string
  display_name?: string
  nickname?: string
  trust_state: 'tofu' | 'verified' | 'conflicted' | 'revoked'
  addresses: string[]
  issued_at: string
  expires_at?: string | null
  updated_at: string
}
```

### 9.3 Message

```ts
interface Message {
  message_id: string
  direction: 'inbound' | 'outbound'
  topic: string
  content_type: string
  payload_base64: string
  text?: string
  from_peer_id?: string
  to_peer_id?: string
  group_id?: string
  session_id?: string
  reply_to?: string
  read?: boolean
  timestamp: string
}
```

说明：

- direct message 的 `text` 可直接由 payload 解码得出。
- group message 的 `group_id`、`text`、逻辑 `content_type` 来自 `group.message.v1` envelope 解析结果。

### 9.4 Group

```ts
interface Group {
  group_id: string
  epoch: number
  max_members: number
  manager_peer_ids: string[]
  local_role?: 'manager' | 'member'
  created_at: string
  updated_at: string
}

interface GroupMember {
  peer_id: string
  role: 'manager' | 'member'
  joined_at: string
  last_seen_at?: string | null
  updated_at: string
}

interface GroupInvite {
  group_id: string
  invite_id: string
  inviter_peer_id: string
  invitee_peer_id: string
  status: 'pending' | 'accepted' | 'rejected' | 'expired'
  created_at: string
  expires_at: string
  responded_at?: string | null
}
```

---

## 10. 部署方式

### 10.1 本地单机（推荐 v1）

目标形态：

```bash
pnpm --dir web dev --port 3000
```

开发期前端默认运行在：

```text
http://127.0.0.1:3000
```

构建并挂到 `aqua serve`：

```bash
pnpm --dir web build
aqua serve --web-console --web-console-addr 127.0.0.1:8089 --console-web-dst-dir ./web/dist
```

访问：

```text
http://127.0.0.1:8089
```

说明：

- 本地模式默认只监听 localhost。
- 开发阶段使用 `pnpm`，本地联调命令为 `pnpm --dir web dev --port 3000`。
- `web/package.json` 中的 `vue` 版本必须与 `quail-ui` 当前兼容版本保持一致。
- 传入 `--console-web-dst-dir` 时，前端静态资源来自 Vue SPA 构建目录。
- `--console-web-dst-dir` 为可选参数；若传入则目录缺失应直接报错，若不传则只开放 API / WebSocket。

### 10.2 远程部署（后续）

- 绑定内网地址或 Tailscale 地址。
- Bearer Token 保护。
- 可选反代。

远程部署不是 v1 必需能力。

---

## 11. 迭代计划

### v1

- Contacts
- Direct Messages
- Node 只读状态页
- 单一事件流 WebSocket
- localhost-only 部署

目标：先把现有 CLI 语义安全搬到 GUI。

### v2

- Group conversation timeline
- Group invites / accept / reject
- 更完整的 relay 状态可视化
- 会话聚合与未读体验完善

目标：把群组与实时体验补齐到“好用”。

### v3

- 远程访问鉴权
- 可写 settings/config 模型
- 若确有必要，再单独设计 supervisor 模式与进程生命周期控制

目标：硬化与运维能力。

---

## 12. 已知约束与风险

| 约束 | 说明 |
| --- | --- |
| Group E2EE 未实现 | 不能在 GUI 中暗示群消息已具备完整 E2EE 保证。 |
| Durable retransmission 未实现 | 发送失败后需要明确提示用户重试。 |
| `group send` 是 sender-side fanout | 成员离线时消息可能失败，UI 必须展示部分失败结果。 |
| `contacts add` 依赖在线拉卡片 | 添加联系人时目标 peer 必须可达。 |
| 当前无 config file 模型 | `Settings` 在 v1 不应假设存在可写持久配置。 |
| 当前无 supervisor | GUI 内不应承诺进程 start/stop。 |
| Multiaddr 校验严格 | 前端必须在提交前校验 `/p2p/<peer_id>` 终端段。 |
| quail-ui 是 Vue 3 库 | 前端框架固定为 Vue 3。 |

---

## 13. 实施备注

建议的实现顺序：

1. 先补 Web Console API 的只读查询与 direct message 发送。
2. 再把 `agent.data.push` / `relay.*` 事件桥接为 WebSocket。
3. 最后接入 group timeline、invite 处理与 settings 展示。

这样可以最大化复用现有 `aqua.Service`、`aqua.Node`、`FileStore` 语义，避免在 GUI 设计阶段引入新的协议层概念。
