# 恢复 PR #322 移除的功能

**状态**: ✅ 已完成

## 背景

PR #322 (https://github.com/Sagit-chu/flvx/pull/322) 原本移除了三个功能，用户要求**加回**这些被移除的功能：
1. 批量操作失败详情弹窗（`BatchOperationFailure` 类型及相关处理）
2. 节点到期提醒关闭功能（`dismissNodeExpiryReminder` API）
3. 更新通道选择功能（稳定版/开发版切换）

用户要求**保留**的改动：
- 版本显示简化（移除 "v" 前缀和更新可用徽章）

## 任务清单

- [x] 检出 PR #322 到本地分支 `pr-322`
- [x] 恢复 `api/types.ts` 中的 `expiryReminderDismissed` 字段
- [x] 恢复 `api/types.ts` 中的 `BatchOperationFailure` 类型和 `failures` 字段
- [x] 恢复 `api/error-message.ts` 中的批量操作失败处理函数
- [x] 恢复 `api/index.ts` 中的 `dismissNodeExpiryReminder` API
- [x] 恢复 `config.tsx` 中的更新通道选择功能
- [x] 恢复 `use-dashboard-data.ts` 中的 `expiryReminderDismissed` 过滤逻辑
- [x] 恢复 `batch-actions.ts` 中的 `BatchOperationFailure` 相关处理
- [x] 恢复 `forward.tsx` 中的 `BatchActionResultModal` 使用
- [x] 恢复 `tunnel.tsx` 中的 `BatchActionResultModal` 使用
- [x] 提交并推送修改

## 修改的文件

- `vite-frontend/src/api/types.ts` - 添加 `expiryReminderDismissed` 和 `BatchOperationFailure`
- `vite-frontend/src/api/error-message.ts` - 添加批量操作失败处理函数
- `vite-frontend/src/api/index.ts` - 添加 `dismissNodeExpiryReminder` API
- `vite-frontend/src/pages/config.tsx` - 添加更新通道选择功能
- `vite-frontend/src/pages/dashboard/use-dashboard-data.ts` - 恢复 `expiryReminderDismissed` 过滤逻辑
- `vite-frontend/src/pages/forward/batch-actions.ts` - 恢复批量操作失败处理
- `vite-frontend/src/pages/forward.tsx` - 恢复 `BatchActionResultModal` 组件使用
- `vite-frontend/src/pages/tunnel.tsx` - 恢复 `BatchActionResultModal` 组件使用
- `vite-frontend/src/pages/node.tsx` - 恢复 `expiryReminderDismissed` 功能和 "关闭提醒" 按钮

## 保留的 UI 改进

- Modal 样式优化（group.tsx, limit.tsx, panel-sharing.tsx）
- 按钮文本简化
- 用户页面隧道列表下拉展开

## forward.tsx 重构审查结果

PR #322 对 forward.tsx 进行了大规模重构（~2200 行 diff），经审查决定**保留**以下改动：

| 改动 | 说明 |
|------|------|
| DnD 碰撞检测 | `closestCenter` → `pointerWithin`，更适合嵌套拖拽 |
| 高级筛选模态框 | 从 SearchBar 改为五合一筛选（名称/用户/隧道/端口/目标地址） |
| 始终显示复选框 | 移除 selectMode 状态，用户无需切换模式即可选择 |
| 组件位置移动 | Sortable 组件移到组件顶部，代码组织更好 |
| UI 改进 | 表头全选、端口独立列、倍率显示优化、Modal 样式、"落地地址"文案 |

## 注意事项

- `version-footer.tsx` 保持简化版本显示（不恢复）
- `batch-action-result-modal.tsx` 组件文件未被 PR 删除，无需恢复（只需恢复 forward.tsx 中的使用）