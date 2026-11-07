# FLVX 安全问题修复设计

**日期**: 2026-05-13
**状态**: 待审核
**作者**: AI Assistant

## 概述

针对 PR #502 提到的安全问题，对 FLVX 后端认证、配置访问控制、配置写入保护、备份导出和 JWT 失效模型做一次集中修复。目标是优先消除高风险漏洞，同时保留当前必须兼容的登录页品牌配置读取和验证码兼容行为。

本设计采用“高危项一次收口，结构性问题只分析不重构”的策略：本轮修复 MD5 密码存储、未受控配置读取、敏感配置写入、备份配置泄露和 JWT 长期有效且改密后不失效的问题；不修改“无 Cloudflare secret 时允许当前 captcha 兼容行为”，也不重构 `autoMigrateAll()` 与 `migrateSchema()` 的双迁移入口。

## 背景

当前主线存在以下已确认问题：

1. `login`、`open_api/sub_store`、用户改密、管理员创建用户和管理员修改用户密码仍然使用 `security.MD5(...)`。
2. `/api/v1/config/get` 在 middleware 的 `shouldSkip()` 中被匿名放行，导致任意调用方可以读取绝大多数配置。
3. `updateConfigs()` 与 `updateSingleConfig()` 使用了两套不同的限制逻辑，敏感配置键在单项写接口中未被保护。
4. `ExportAll()` 和 `ExportPartial(types=["configs"])` 会直接导出所有配置，包含 `jwt_secret`、`license_key`、`cloudflare_secret_key`。
5. JWT 当前有效期为 90 天，且 token 在用户改密、禁用、角色变化后仍可继续使用到过期。

同时存在两个重要约束：

1. 登录页和未登录态品牌展示依赖匿名读取 `app_name`、`app_logo`、`app_favicon`、`app_bg_image` 和 `cloudflare_site_key`。
2. `tests/contract/migration_contract_test.go` 已把“无 Cloudflare secret 时允许当前 captcha 兼容行为”定义为既有契约，本轮不改变。

## 目标

1. 新增和更新后的用户密码不再以 MD5 存储。
2. 历史 MD5 用户可在首次成功认证时自动迁移到强哈希。
3. 匿名请求不能再读取任意配置，只能读取明确的公开配置白名单。
4. 通用配置写接口不能覆盖敏感配置键。
5. 备份导出默认不泄露敏感配置明文。
6. 用户改密、禁用或角色变化后，旧 JWT 应立即失效。
7. 不破坏现有登录页品牌展示和 captcha 兼容行为。

## 非目标

1. 不重构 `open_api/sub_store` 的整体认证模型；该接口仍使用现有用户名和密码查询参数语义。
2. 不实现完整 refresh token、session 管理后台或 token 黑名单体系。
3. 不改变“无 Cloudflare secret 时允许当前 captcha 兼容行为”。
4. 不在本轮重构 `autoMigrateAll()` 与 `migrateSchema()` 的启动流程。
5. 不引入前端测试框架。

## 影响范围

### 后端

- `go-backend/internal/security/`
- `go-backend/internal/auth/jwt.go`
- `go-backend/internal/http/middleware/auth.go`
- `go-backend/internal/http/handler/handler.go`
- `go-backend/internal/http/handler/mutations.go`
- `go-backend/internal/store/model/model.go`
- `go-backend/internal/store/repo/repository.go`
- `go-backend/internal/store/repo/repository_mutations.go`
- `go-backend/tests/contract/`
- `go-backend/internal/store/repo/*_test.go`

### 前端

- `vite-frontend/src/api/index.ts`
- `vite-frontend/src/config/site.ts`
- `vite-frontend/src/pages/index.tsx`
- 任何在未登录态读取品牌配置的组件

## 设计决策

### 已确认决策

1. 本轮采用安全优先策略，允许收紧危险默认行为。
2. MD5 密码采用“登录成功时自动迁移”的兼容方案。
3. captcha 在未配置 Cloudflare secret 时的兼容行为保持不变。
4. JWT 采用“最小可撤销”方案，而不是完整 session 体系。
5. 双重迁移系统只分析，不在本轮中修改。

### 迁移系统分析结论

`autoMigrateAll()` 与 `migrateSchema()` 当前职责并不相同：

1. `autoMigrateAll()` 负责表和列结构补齐。
2. `migrateSchema()` 负责基于 `schema_version` 的数据修正，以及 PostgreSQL ID 默认值修复等兼容迁移。
3. 现有 `repository_migrate_test.go` 已明确覆盖这两部分逻辑，说明它们在现有代码库中是被依赖的互补结构，而不是已确认的重复安全漏洞。

因此本轮仅记录该分析结论，不把双迁移入口纳入改动范围，避免把安全修复扩展为启动流程重构。

## 详细设计

### 1. 密码存储与认证迁移

在 `internal/security/` 中新增统一密码能力，替代各处直接使用 `security.MD5(...)` 的做法。

建议新增以下接口：

```go
func HashPassword(plain string) (string, error)
func VerifyPassword(storedHash, plain string) (ok bool, legacy bool)
func IsLegacyPasswordHash(storedHash string) bool
```

哈希算法使用 `bcrypt`：

1. `user.pwd` 当前为 `varchar(100)`，足以容纳 bcrypt 哈希。
2. 不需要修改密码列长度，改动最小。
3. 对当前 Go 后端来说，bcrypt 是最稳妥的强哈希升级路径。

所有密码入口统一改为走这套能力：

1. `login`
2. `openAPISubStore`
3. `updatePassword`
4. `userCreate`
5. `userUpdate` 中的管理员改密路径

认证迁移规则：

1. 如果数据库中存的是 bcrypt，则按 bcrypt 校验。
2. 如果数据库中存的是历史 MD5，则先按旧逻辑校验。
3. 历史 MD5 校验成功后，立即把 `pwd` 改写为 bcrypt。
4. 自动迁移不仅在网页登录时执行，也在 `open_api/sub_store` 成功鉴权时执行，避免只使用订阅接口的老用户永远停留在 MD5。

默认管理员种子账号仍保留当前默认密码语义和 `requirePasswordChange` 行为，但种子哈希改为 bcrypt，不再在新建数据库中写入 MD5 值。

### 2. JWT 最小可撤销方案

本轮不引入 refresh token 和黑名单表，而是做一个可以立即生效的最小撤销闭环。

#### 数据模型

在 `user` 表新增字段：

- `password_changed_at BIGINT NOT NULL DEFAULT 0`

该字段专门表示密码最后一次变更时间，不能复用现有 `updated_time`，原因是 `updated_time` 还会被流量、状态或其他用户资料更新触发，复用后会让非密码更新错误地使 token 失效。

#### token 签发与校验

继续使用现有 `iat` 声明，但把有效期从 90 天收紧到 7 天。

token 校验分两步：

1. 先做现有签名和过期时间校验。
2. 再读取用户最小认证状态，确认：
   - 用户仍存在
   - 用户状态未被禁用
   - 当前 `role_id` 与 token 中一致
   - `claims.iat` 不早于 `password_changed_at`

为避免 middleware 每次都查询完整用户对象，Repository 新增专用读取方法，只返回 token 校验需要的最小字段，例如：

```go
type UserAuthState struct {
    ID                int64
    RoleID            int
    Status            int
    PasswordChangedAt int64
}

func (r *Repository) GetUserAuthState(userID int64) (*UserAuthState, error)
```

#### 失效语义

以下场景下，旧 token 应立即失效：

1. 用户修改密码
2. 管理员修改用户密码
3. 用户被禁用
4. 用户角色发生变化

这会带来一次明确的兼容收紧：升级完成后，部分历史 token 可能因为寿命策略或认证状态变化而失效，这是安全优先下的可接受行为。

### 3. 配置读取访问控制

为了避免继续让 `/api/v1/config/get` 承担“有时匿名、有时鉴权”的混合语义，本设计将公开配置读取拆成单独的 public 端点。

#### 端点设计

保留现有受保护端点：

- `POST /api/v1/config/get`

新增公开端点：

- `POST /api/v1/public/config/get`

middleware 仅对白名单 public 端点放行，不再放行 `/api/v1/config/get`。

#### 公开白名单

匿名仅允许读取以下配置：

1. `app_name`
2. `app_logo`
3. `app_favicon`
4. `app_bg_image`
5. `cloudflare_site_key`

理由：

1. 登录页与未登录态品牌渲染依赖前四项。
2. 登录页在 captcha 开启时需要读取 `cloudflare_site_key`。
3. 其他配置不应暴露给匿名方。

前端调整规则：

1. 登录页和 `site.ts` 中的未登录态品牌配置读取改走 `/public/config/get`。
2. 登录后页面仍使用现有 `/config/get` 或 `/config/list`。
3. 已登录页面中的配置读取逻辑不变，只是恢复为真正受 JWT 保护。

### 4. 配置写保护统一

当前 `updateConfigs()` 与 `updateSingleConfig()` 各自维护不同限制逻辑，是本次越权写入漏洞的根源。本轮把配置访问规则统一收口为一套辅助函数。

建议新增配置策略定义：

```go
type ConfigAccessPolicy struct {
    PublicReadable bool
    Sensitive      bool
    CommercialOnly bool
}
```

由统一函数返回某个 key 的策略，再由：

1. `public config get`
2. `config get`
3. `config list`
4. `updateConfigs()`
5. `updateSingleConfig()`

共同复用。

敏感配置键至少包含：

1. `jwt_secret`
2. `license_key`
3. `cloudflare_secret_key`

这些键的写入规则：

1. 不允许通过通用配置写接口改写。
2. 不允许通过公开读取接口读取。
3. 非管理员在配置列表接口中也不能获得。

商业版白名单键继续沿用现有语义，例如：

1. `app_name`
2. `app_logo`
3. `app_favicon`
4. `hide_footer_brand`

但其判断逻辑同样统一走同一套策略函数，避免再次出现单接口漏判。

### 5. 备份导出与导入脱敏

备份系统改为“默认安全导出”，而不是“完整明文镜像”。

#### 导出

`ExportAll()` 和 `ExportPartial(types=["configs"])` 在写入 `backup.Configs` 前都先经过统一过滤函数，移除敏感配置键。

敏感配置键与配置写保护列表保持一致：

1. `jwt_secret`
2. `license_key`
3. `cloudflare_secret_key`

#### 导入

导入配置时，即使旧备份中带有上述敏感键，也会在导入前被丢弃，不允许通过备份恢复路径覆盖在线安全配置。

该设计的取舍如下：

1. 保留大部分业务配置、节点、转发、用户数据的恢复能力。
2. 不再把备份文件当作核心密钥分发载体。
3. `UserBackup.Pwd` 仍然保留，以维持用户恢复语义；在本轮密码升级后，这些值将是 bcrypt 哈希，而不是 MD5。

### 6. captcha 兼容行为

`captcha_enabled`、`cloudflare_site_key`、`cloudflare_secret_key` 的现有兼容行为保持不变。

明确保持以下现状：

1. 当未完整配置 Cloudflare key 时，当前 contract test 约定的兼容路径继续存在。
2. 本轮不把 captcha 兼容逻辑从“兼容旧行为”切换为“严格校验”。

这样可以避免把一轮安全修复扩展成登录流程行为变更，同时与用户已确认的范围保持一致。

## 错误处理与兼容行为

### 错误处理

保持现有 API envelope：`{code, msg, data, ts}`。

建议的接口行为：

1. `POST /api/v1/public/config/get` 请求非公开 key 时返回 `403`。
2. 受保护配置端点未登录时返回 `401`。
3. 登录、订阅接口、改密接口继续返回通用认证失败，不暴露“用户名存在但密码错误”等细节。
4. token 因签名错误、过期、改密、禁用或角色变化失效时，统一返回现有 `401` 语义。
5. 备份导入中出现敏感配置键时，接口整体仍允许成功导入其他数据，敏感键静默忽略。

### 保留兼容

1. 登录页和未登录态品牌展示继续可用。
2. 未配置 Cloudflare secret 时的 captcha 兼容逻辑继续保留。
3. 历史 MD5 用户仍可继续认证，并在成功后自动迁移。

### 刻意收紧

1. 匿名方不再可读取任意配置。
2. 通用配置写接口不再能写入敏感键。
3. 备份不再导出敏感配置明文。
4. 改密、禁用和角色变化会立即使旧 token 失效。

## 测试设计

本轮以 Go 单测和 contract test 为主，覆盖以下场景。

### 密码迁移

1. 历史 MD5 用户在网页登录成功后，数据库中的 `pwd` 被升级为 bcrypt。
2. 历史 MD5 用户在 `open_api/sub_store` 成功鉴权后，同样触发迁移。
3. 新建用户后落库的是 bcrypt，而不是 MD5。
4. 管理员修改用户密码和用户自助改密后，落库的是 bcrypt。

### JWT 最小可撤销

1. 正常 token 仍可访问受保护接口。
2. 改密后旧 token 失效。
3. 用户被禁用后旧 token 失效。
4. 用户角色变化后旧 token 失效。
5. 过期 token 失效。

### 配置访问控制

1. 匿名访问公开配置成功。
2. 匿名访问非公开配置失败。
3. 已登录页面需要的普通配置读取仍然可用。
4. `updateSingleConfig()` 无法修改敏感键。
5. `updateConfigs()` 同样无法修改敏感键。

### 备份脱敏

1. `ExportAll()` 不包含敏感配置。
2. `ExportPartial(types=["configs"])` 不包含敏感配置。
3. 导入带敏感键的备份时，这些键不会被写回数据库。
4. 非敏感配置和其他业务数据仍可正常导入导出。

### 迁移系统回归

1. 现有 `repository_migrate_test.go` 保持通过。
2. 本轮不对 `autoMigrateAll()` 与 `migrateSchema()` 的职责边界做行为性改动。

## 验收标准

1. 数据库中不再新增 MD5 密码。
2. 历史 MD5 用户可在首次成功认证后自动升级到 bcrypt。
3. 匿名调用方不能再读取非公开配置。
4. `updateSingleConfig()` 和 `updateConfigs()` 都无法改写敏感配置键。
5. 备份导出默认不包含 `jwt_secret`、`license_key`、`cloudflare_secret_key`。
6. 改密、禁用和角色变化后，旧 JWT 立即失效。
7. 登录页品牌展示和 captcha 兼容行为不被破坏。
8. 现有迁移测试和本轮新增安全测试全部通过。

## PR #502 处置

PR #502 的价值在于指出了真实问题，但其实现方式只是把讽刺性注释写进生产代码，并未修复漏洞。因此该 PR 不应合并。

执行阶段的处置方式：

1. 关闭 PR #502。
2. 在关闭说明中指出：问题成立，但修复将通过正式代码与测试提交完成，而不是通过向源文件加入讽刺性注释。
3. 后续在新提交中按本设计逐项修复。
