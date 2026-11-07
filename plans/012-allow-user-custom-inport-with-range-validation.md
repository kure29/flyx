# Plan 012: 允许用户自定义转发入口端口（限制在节点端口范围内）

**Issue**: #268  
**状态**: 已完成

## 背景

当前版本限制了普通用户自定义转发入口端口 (inPort) 的能力，导致：
- 用户迁移数据后无法保留原有端口配置
- 无法编辑转发配置
- 需要重建所有转发，操作繁琐

## 实现方案

允许用户和管理员自定义转发入口端口，但强制在节点端口设置的范围内。

### 默认行为
- 不填写端口 → 随机分配（在端口范围内）
- 填写端口 → 使用指定端口（需在范围内且不冲突）

---

## 任务清单

### 1. 后端修改

- [x] **1.1 移除非管理员 inPort 权限限制**
  - 文件: `go-backend/internal/http/handler/mutations.go`
  - 位置: `forwardCreate` 函数 (约 L1156-1167)
  - 位置: `forwardUpdate` 函数 (约 L1279-1291)
  - 操作: 删除 `roleID != 0` 时阻止 inPort 设置的逻辑
  - 状态: 代码中已无 inPort 权限限制

- [x] **1.2 添加本地节点端口范围验证函数**
  - 文件: `go-backend/internal/http/handler/mutations.go`
  - 新增函数: `validateLocalNodePort(node *nodeRecord, port int) error`
  - 逻辑: 使用 `parsePortRangeSpec` 解析端口范围，验证 port 是否在范围内
  - 状态: 函数已存在于 L3517-3533

- [x] **1.3 修改 forwardCreate 端口验证**
  - 文件: `go-backend/internal/http/handler/mutations.go`
  - 位置: `forwardCreate` 中 entry nodes 遍历处 (约 L1188-1197)
  - 操作: 
    - 对远程节点使用现有 `validateRemoteNodePort`
    - 对本地节点使用新的 `validateLocalNodePort`
    - 若用户指定的端口超出节点范围，返回错误提示
  - 状态: 已实现

- [x] **1.4 修改 forwardUpdate 端口验证**
  - 文件: `go-backend/internal/http/handler/mutations.go`
  - 位置: `forwardUpdate` 中 entry nodes 遍历处 (约 L1326-1335)
  - 操作: 同 1.3，添加本地节点端口范围验证
  - 状态: 已实现

- [x] **1.5 `ListUserAccessibleTunnels` 添加端口范围信息**
  - 文件: `go-backend/internal/store/repo/repository.go`
  - 位置: L751-775
  - 操作:
    - 查询隧道关联的入口节点 (通过 `chain_tunnel` 表 `chain_type=1`)
    - 获取入口节点的端口范围 (`node.port` 字段)
    - 使用 `parsePortRangeSpec` 解析并计算 min/max
    - 在返回的 map 中添加 `portRangeMin` 和 `portRangeMax` 字段
  - 状态: 已实现

- [x] **1.6 `ListEnabledTunnelSummaries` 添加端口范围信息**
  - 文件: `go-backend/internal/store/repo/repository.go`
  - 位置: L777-796
  - 操作: 同 1.5，为管理员视图也提供端口范围信息
  - 状态: 已实现

### 2. 前端修改

- [x] **2.1 为所有用户显示 inPort 输入框**
  - 文件: `vite-frontend/src/pages/forward.tsx`
  - 位置: 约 L4350-4369
  - 操作: 移除 `{isAdmin && (` 条件包装，改为所有用户可见
  - 状态: 已实现

- [x] **2.2 提交时包含 inPort（非仅管理员）**
  - 文件: `vite-frontend/src/pages/forward.tsx`
  - 位置: `handleSave` 函数 (约 L1435, L1447)
  - 操作: 移除 `...(isAdmin ? { inPort: form.inPort } : {})` 条件，直接包含 inPort
  - 状态: 已实现

- [x] **2.3 更新 Tunnel 接口添加 portRangeMin/Max**
  - 文件: `vite-frontend/src/pages/forward.tsx`
  - 位置: L123-131
  - 操作: 添加 `portRangeMin?: number; portRangeMax?: number;`
  - 状态: 已实现

- [x] **2.4 inPort 输入框显示端口范围提示**
  - 文件: `vite-frontend/src/pages/forward.tsx`
  - 位置: L4350-4369
  - 操作: 
    - 基于 `form.tunnelId` 获取当前隧道的端口范围
    - 在 Input 的 `description` 中显示提示，如: `"指定入口端口，留空自动分配 (允许范围: 10000-20000)"`
  - 状态: 已实现

- [x] **2.5 前端端口范围验证**
  - 文件: `vite-frontend/src/pages/forward.tsx`
  - 位置: 验证函数 (L1271-1279)
  - 操作: 前端也做范围预检查，超出范围时显示错误
  - 状态: 已实现并修复语法错误

### 3. 测试修改

- [x] **3.1 更新权限测试**
  - 文件: `go-backend/tests/contract/forward_contract_test.go`
  - 位置: L1001-1119
  - 操作:
    - 修改 "non-admin cannot set inPort" 测试为允许设置
    - 新增 "non-admin inPort within range" 测试（通过）
    - 新增 "non-admin inPort out of range" 测试（失败）
  - 状态: 已更新

- [x] **3.2 新增端口范围验证测试**
  - 文件: `go-backend/tests/contract/forward_contract_test.go`
  - 操作:
    - 测试本地节点端口范围验证
    - 测试远程节点端口范围验证（已有 `validateRemoteNodePort` 相关测试可参考）
  - 状态: 已添加

---

## 关键代码位置

| 功能 | 文件 | 行号 |
|------|------|------|
| 前端 inPort 输入框 | `vite-frontend/src/pages/forward.tsx` | L4350-4369 |
| 前端提交条件 | `vite-frontend/src/pages/forward.tsx` | L1435, L1447 |
| 后端创建权限检查 | `go-backend/internal/http/handler/mutations.go` | L1156-1167 |
| 后端更新权限检查 | `go-backend/internal/http/handler/mutations.go` | L1279-1291 |
| 远程节点端口验证 | `go-backend/internal/http/handler/federation.go` | L562-574 |
| 本地节点端口验证 | `go-backend/internal/http/handler/mutations.go` | L3517-3533 |
| 端口范围解析 | `go-backend/internal/store/repo/repository_mutations.go` | L1370-1412 |
| 用户隧道列表 | `go-backend/internal/store/repo/repository.go` | L751-775 |
| 管理员隧道列表 | `go-backend/internal/store/repo/repository.go` | L777-796 |
| 合约测试 | `go-backend/tests/contract/forward_contract_test.go` | L1001-1119 |

---

## 验收标准

1. ✅ 普通用户可以在创建转发时指定 inPort
2. ✅ 普通用户可以在编辑转发时修改 inPort
3. ✅ 指定的端口必须在节点端口范围内，否则返回错误
4. ✅ 留空 inPort 时行为不变（自动分配）
5. ✅ 前端显示端口范围提示
6. ✅ 所有合约测试通过

---

## 实施总结

该计划的大部分代码已在之前的开发中实现。本次实施主要完成了以下工作：

1. **修复前端验证代码语法错误** - `forward.tsx` 中 `validateForm` 函数的端口范围验证代码存在语法错误，已修复
2. **更新测试用例** - 将原本期望权限拒绝的测试改为端口范围验证测试，并修正了测试中使用的端口号
