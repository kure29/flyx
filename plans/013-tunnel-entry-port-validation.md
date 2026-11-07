# 013 - 隧道入口端口校验不严格修复

## Issue
- GitHub Issue: [#373](https://github.com/Sagit-chu/flvx/issues/373)

## 修复方案

在 `syncTunnelForwardsEntryPorts` 中实现逐节点端口分配：

- **旧入口节点**：保留原端口不变
- **新入口节点**：通过 `resolvePortForNewEntryNode` 决策：
  - 参考端口在范围内且未被占用 → 跟随设置一样的端口
  - 参考端口超出范围或被占用 → 通过 `pickRandomPortForNode` 为该节点单独随机分配

## 任务清单

- [x] 1. 实现 `pickRandomPortForNode` 辅助方法（单节点端口随机分配）
- [x] 2. 实现 `resolvePortForNewEntryNode` 方法（端口决策逻辑）
- [x] 3. 重写 `syncTunnelForwardsEntryPorts` 为逐节点分配
- [x] 4. 移除不再需要的 `isPortValidForAllEntryNodes`
- [x] 5. 构建通过 + 全量测试通过
