# ReOpenOldChatServer 协议升级策划：ncuid 体系 + v2 接口

> 依据：`docs/mcl0-docs-0815/in_api.md`（官方 v2 文档，1268 行）、`docs/nx10.md`（nx10 逆向，5129 行）、`docs/mcl0-docs-0815/client-guide.md`
> 参照客户端：`D:\oldchat-kivotos-next-app\src`
> 现状代码：Go，29k 行，`internal/http/api.go` 集中路由

---

## 一、差距总览（现状 vs 新协议）

| # | 能力 | 新协议要求 | 服务器现状 | 缺口等级 |
|---|---|---|---|---|
| 1 | ncuid 体系 | 用户/消息/成员/请求全带 `*_ncuid`；初始 NCUID = 初始 UID；UID 可变，**NCUID 不可变** | **全库零 ncuid**（`grep -rn ncuid` 结果为空） | P0 阻断 |
| 2 | `/v2/*` 路径前缀 | 全部业务接口迁到 `/v2/xxx` | 只有 `/direct/messages/v2`、`/groups/messages/v2` 两个老式命名，**无 `/v2/` 前缀路由** | P0 阻断 |
| 3 | `POST /v2/gateway` | 统一网关，所有 `/v2` 请求折叠到一个入口，隐藏真实路径 | 无 | P0 阻断 |
| 4 | 加密信封 `X-Enc: 1` | `{iv,data,mac}` AES-256-CBC + HMAC-SHA256，支持 gzip | 已具备底层：`internal/secure/crypto.go` 有 `Encrypt/Decrypt/DeriveSessionKeys`，**未接 `X-Enc` 头与 gzip** | P0 小改 |
| 5 | `GET /v2/updates/difference?pts=` | 账号级单调游标 pts 事件流，断线补差 | 无 pts 事件表；现状是 WS 推送 + 轮询 | P1 核心 |
| 6 | `GET /v2/groups/events/after` | 群事件序列 `group_seq` 补差 | 部分有 group_seq，无事件通道接口 | P1 核心 |
| 7 | `GET /v2/groups/messages/after` | 按 `after_seq` 拉群消息 | 无 | P1 |
| 8 | `POST /v2/unread/direct` `/v2/unread/groups` | 未读聚合 | 无同名接口 | P1 |
| 9 | friends / groups / direct 系列 v2 化 | `/v2/friends`、`/v2/groups/list`、`/v2/direct/send` … | v1 已实现，需路径 + 字段双轨 | P1 批量 |
| 10 | 频道 channels | `/v2/channels/{discover,state,states,posts/after,events/after}` | **完全不存在**（`grep channel` 仅命中 Go channel 关键字） | P2 新模块 |
| 11 | 阅后即焚 `/v2/*/burn/open`、邀请 `/v2/groups/invitations`、`members/lookup` | 新增 | 部分（burn 有字段，无 open 接口） | P2 |

---

## 二、ncuid 体系设计（最关键）

### 语义
- `uid`：对外展示/加好友用，**可修改**（现状已有 `uid_changed_at` + 冷却 `ErrUIDTooSoon`）。
- `ncuid`：系统内部标识，**不可修改、全局唯一**，注册时生成且 **`ncuid = 初始 uid`**（`USR-XXXXXXXX`，`newPublicID("USR-", 8)`，见 `internal/http/auth_handlers.go:176`）。
- 客户端本地 SQLite 已为 ncuid 预留列（nx10.md:2364-2488：`direct_message.from_ncuid`、`direct_read.reader_ncuid`、`group_message.from_ncuid`、`group_members.ncuid` 且 `PRIMARY KEY(account, group_id, ncuid)`）。

### 落地方案
1. **schema 迁移**（`internal/data/schema.sql` 增列，全部 `TEXT NOT NULL DEFAULT ''`，避免破坏旧库）：
   - `users.ncuid`（UNIQUE）
   - `direct_messages.from_ncuid` / `to_ncuid`
   - `group_messages.from_ncuid`
   - `group_members.ncuid`
   - `friend_requests.from_ncuid` / `to_ncuid`、`friends.friend_ncuid`
   - `moments.author_ncuid`、`notifications.*`（按文档补）
2. **回填**：启动时一次性 `UPDATE users SET ncuid = uid WHERE ncuid = ''`；消息表按 `from_uid → users.ncuid` 回填。
3. **规范层**：`data.User` 增 `NCUID string \`db:"ncuid"\`` 与 JSON `ncuid`；所有用户型响应统一补字段。
4. **解析优先级**：请求里 `*_ncuid` 与 `*_uid` 同时出现时，**以 ncuid 为准**（ncuid 不可变，uid 可能被改过）；只给 uid 时查 `users.ncuid` 转换。
5. **不变性守卫**：封禁所有改 ncuid 的入口（管理端 `new_uid` 只改 uid，禁止触碰 ncuid）。

### 风险点
- 现有部署：`metrochat.db` 里已有用户，回填后 ncuid = 当前 uid（可能与"初始 uid"不同，若用户改过 uid）。**可接受**（ncuid 唯一即可），但需一次性记录回填日志备查。

---

## 三、v2 路由与网关

### 3.1 双栈策略（推荐）
- **保留全部 v1 路径**，新增 `/v2/*` 前缀路由，内部 handler 复用，只在响应层做字段补齐。
- 好处：旧客户端（nx10 之前）不炸；新客户端走 v2。
- 实现：`api.go` 里抽 `registerV2(r)`，把 v1 handler 批量挂两处；差异大的（消息拉取分页、friends）单独写 v2 handler。

### 3.2 `/v2/gateway`
```
POST /v2/gateway
body(明文或加密) = { "m":"POST", "p":"/v2/groups/messages/v2", "q":"group_id=..&limit=20", "b":{...} }
resp = HTTP 200 + { "code":200, "body":{...} }
```
- 实现：解析后内部重放请求到 chi router（`chi.NewRouteContext()` + `router.ServeHTTP`），不落网。
- 强制 `p` 必须以 `/v2/` 开头，否则 400。
- 响应固定 HTTP 200，业务错误码放 `code`。

### 3.3 加密信封
- 请求头 `X-Enc: 1` → 解密 body；`X-Enc-Compression: gzip` → 解 gzip。
- 响应对称处理（≥512B 时 gzip 再加密）。
- 复用 `internal/secure/crypto.go`（AES-256-CBC + PKCS7 + HMAC-SHA256，恒定时间比较已具备）。
- 中间件位置：`secure_middleware.go`，且必须在网关解包之后、业务 handler 之前。

---

## 四、事件流（pts）

### 新增表
```sql
CREATE TABLE IF NOT EXISTS updates (
  pts INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id TEXT NOT NULL,
  type TEXT NOT NULL,          -- DIRECT_MESSAGE_NEW / GROUP_MESSAGE_NEW / FRIEND_* / ...
  date INTEGER NOT NULL,
  payload TEXT NOT NULL        -- JSON
);
CREATE INDEX IF NOT EXISTS idx_updates_account_pts ON updates(account_id, pts);
```
- 写入点：所有产生状态变更的 handler（发消息、已读、好友、红包、moment、撤回、通知）。
- `GET /v2/updates/difference?pts=N&limit=200`：`has_more` / `next_pts` / `current_pts` / `reset`。
- reset 三条判定照文档实现（差距 >10000 / 已归档 / 请求 pts > 当前 pts）。
- 保留旧 WS 推送不动，二者并存。

### 群事件
- `GET /v2/groups/events/after?group_id=&seq=N`：合并 `MESSAGE_NEW` / `RECALL` / `READ`，每项 `{group_seq, event_type, payload}`。
- `GET /v2/groups/messages/after?group_id=&seq=&limit=`：返回 `{messages, server_group_seq}`。

---

## 五、实施阶段（建议顺序）

| 阶段 | 内容 | 产出可验证点 | 依赖 |
|---|---|---|---|
| **0. 探测对齐** | 用现有账号打官方 `oc.mcl0.dpdns.org` 的 `/v2/*` 真实接口，抓实际字段与错误码 | 一份「文档 vs 线上」差异表 | 无 |
| **1. ncuid 落地** | schema 增列 + 回填 + 响应补字段 + 解析优先级 | `/v2/...` 任一响应含 `ncuid` | 无 |
| **2. v2 路由 + 加密信封** | `registerV2` + `X-Enc` 中间件 | v1/v2 双栈同时可用 | 1 |
| **3. gateway** | `/v2/gateway` 内部重放 | 折叠请求返回 `code:200` | 2 |
| **4. 事件流** | updates 表 + difference + 群事件 + unread | 断线重连续拉不丢 | 1、2 |
| **5. channels** | 频道模块（全新表 + 5 个接口） | discover/posts/events 通 | 4 |
| **6. P2 补全** | burn/open、invitations、members/lookup、撤回 | 客户端无 404 | 3-5 |

---

## 六、需要你先拍板的 3 件事

1. **是否探测官方线上**：拿 `LGCR837` 账号打 `oc.mcl0.dpdns.org` 的 v2 接口拿真实报文。文档有滞后风险，探测能把"字段到底叫什么"钉死。**建议做**，但会产生真实请求（发消息/读列表）。
2. **v1 是否保留**：推荐双栈保留。若你只想保新协议，可以砍掉 v1 路由，代码更少但旧客户端全废。
3. **数据库迁移方式**：`ALTER TABLE ADD COLUMN` 原地升级（推荐，旧库直接可用）vs 新建库 + 导入脚本（干净但要停服）。

---

## 七、已知不确定项（需阶段 0 验证）

- `ncuid` 是否也用 `USR-` 前缀（文档示例写 `"ncuid": "USR-XXX"`，但另一处示例为 `"USER_NCUID"`，**两处不一致**）。
- `/v2/gateway` 是否是**唯一**入口（文档说"所有请求都可折叠"，非强制）。
- channels 模块是否已在你目标客户端启用（第三方客户端 `api.js` 里**搜不到 ncuid**，可能该客户端尚未升级，需确认目标客户端版本）。
