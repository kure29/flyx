# 069: UDP 隧道转发断流修复 + KCP 协议支持

## 问题分析

GOST 隧道转发 UDP 断流的根因涉及三个超时层：

### 1. UDP 虚拟连接 TTL 过短（5秒）— 主因
- `go-gost/x/listener/udp/metadata.go:11`: `defaultTTL = 5s`
- `go-gost/x/internal/net/udp/pool.go:79-115`: `idleCheck()` 每 TTL 周期标记空闲连接，下一周期关闭
- **影响**: UDP 流无数据包超过 ~10 秒即被静默关闭
- **后端配置**: `control_plane.go:1607-1613` 设置了 `keepAlive: true`，但仅在 `tunnelTLSProtocol=true` 时设 `ttl: "10s"`，常规隧道转发不设 TTL 覆盖

### 2. Smux Keepalive 未显式配置
- smux `DefaultConfig()`: `KeepAliveInterval=10s`, `KeepAliveTimeout=30s`
- 后端 `buildTunnelChainServiceConfig` 生成 relay handler 时不设置 mux 参数
- 依赖 smux 默认值，但中间 NAT/防火墙可能先于 30s 断开 TCP 连接

### 3. 隧道仅依赖 TCP 传输 — 无 UDP 备选
- 隧道链节点间仅支持 TLS/TCP 传输
- TCP 队头阻塞 + 单连接承载所有流 + 无 UDP 级别的容错

## 修复方案

### Part A: 修复现有问题（低风险，即时生效）

#### A1. 增大 UDP listener 默认 TTL
- **文件**: `go-gost/x/listener/udp/metadata.go`
- **改动**: `defaultTTL = 30s`（从 5s）
- **原因**: 5s 对实际场景太激进，DNS 等长间隔 UDP 应用会被误杀

#### A2. 后端生成 UDP forward 配置时强制设置 TTL
- **文件**: `go-backend/internal/http/handler/control_plane.go`
- **改动**: `buildForwardServiceConfigs` 中始终设置 `listener.metadata.ttl = "30s"`
- **原因**: 确保配置显式覆盖，不依赖 go-gost 默认值

#### A3. 后端隧道链服务配置加入 mux keepalive
- **文件**: `go-backend/internal/http/handler/mutations.go`
- **改动**: `buildTunnelChainServiceConfig` 中的 relay handler 加入 mux metadata:
  ```go
  "mux.keepaliveInterval": "15s",
  "mux.keepaliveTimeout":  "45s",
  ```

### Part B: 新增 KCP 协议支持（新功能）

#### B1. 后端支持 `kcp` 作为隧道链传输协议
- **文件**: `go-backend/internal/http/handler/mutations.go`
- **改动**:
  - `buildTunnelChainConfig`: 识别 `kcp` 协议，生成对应的 connector/dialer
  - `buildTunnelChainServiceConfig`: 识别 `kcp` 协议，生成 KCP listener
  - `isTLSTunnelProtocol` → `isTCPTunnelProtocol`（包含 TLS）
  - KCP dialer 设置 `kcp.keepalive` 参数
  - KCP 配置无需 `nodelay`（那是 TLS 特有）

#### B2. 前端支持选择 `kcp` 协议
- **文件**: `vite-frontend/src/pages/tunnel/` 相关表单
- **改动**: 协议选择器中加入 KCP 选项

#### B3. 确认 go-gost KCP 组件正常
- KCP dialer: `go-gost/x/dialer/kcp/` — 已存在
- KCP listener: `go-gost/x/listener/kcp/` — 已存在
- KCP connector: 使用 `relay` connector 通过 KCP dialer 连接
- 验证 KCP 配置的 metadata 参数传递正确

## 任务清单

- [x] A1: 增大 UDP listener 默认 TTL (5s→30s)
- [x] A2: 后端 forward 配置强制设置 UDP TTL=30s
- [x] A3: 后端隧道链 relay handler 加入 mux keepalive
- [x] B1: 后端支持 kcp 作为隧道链协议
- [x] B2: 前端协议选择器加入 KCP
- [x] B3: 验证 go-gost KCP 组件配置正确
- [x] C1: go-backend 合约测试 (187 passed)
- [x] C2: go-gost 编译验证 (passed)
- [x] C3: vite-frontend 编译验证 (passed)

## 文件变更清单

| 文件 | 变更类型 |
|------|----------|
| `go-gost/x/listener/udp/metadata.go` | 修改 defaultTTL: 5s→30s |
| `go-gost/x/listener/rudp/metadata.go` | 修改 defaultTTL: 5s→30s |
| `go-backend/internal/http/handler/control_plane.go` | buildForwardServiceConfigs: 强制设置 UDP TTL=30s, 移除 tunnelTLSProtocol 参数 |
| `go-backend/internal/http/handler/control_plane_test.go` | 更新测试调用签名 |
| `go-backend/internal/http/handler/mutations.go` | 新增 isTCPTunnelProtocol/isKCPTunnelProtocol/buildTunnelDialerConfig/buildTunnelListenerConfig; 隧道链配置加入 mux keepalive + KCP 支持 |
| `go-backend/internal/http/handler/federation.go` | federation 配置同步加入 mux keepalive + KCP 支持 |
| `vite-frontend/src/pages/tunnel.tsx` | 协议选择器新增 KCP 选项 (3 处)
