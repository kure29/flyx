# TypeScript Type Definitions

## API Response Envelope

```typescript
interface APIResponse<T = unknown> {
  code: number;      // 0 = success
  msg: string;       // Message (usually Chinese)
  ts: number;        // Unix timestamp in milliseconds
  data?: T;          // Response payload
}
```

## Pagination

```typescript
interface PaginatedRequest {
  page?: number;
  pageSize?: number;
  keyword?: string;
}

interface PaginatedResponse<T> {
  list: T[];
  total: number;
}
```

## User

```typescript
interface User {
  id: number;
  user: string;           // Username
  pwd?: string;           // Password (only on create/update)
  name?: string;          // Display name
  role_id: number;        // 0 = admin, 1 = regular
  status: number;         // 1 = active, 0 = disabled
  flow: number;           // Traffic quota in GB
  in_flow: number;        // Used upload in bytes
  out_flow: number;       // Used download in bytes
  exp_time: number;       // Expiry timestamp (ms), 0 = never
  flow_reset_time: number;// Monthly reset day (1-28), 0 = no reset
  created_at?: number;
  updated_at?: number;
}

interface UserCreateRequest {
  user: string;
  pwd: string;
  name?: string;
  status?: number;
  flow?: number;
  num?: number;
  expTime?: number;
  flowResetTime?: number;
  groupIds?: number[];
}

interface UserPackage {
  flow: number;           // Total quota in GB
  inFlow: number;         // Used upload in bytes
  outFlow: number;        // Used download in bytes
  tunnels: number;        // Assigned tunnel count
  forwards: number;       // Created forward count
  expTime: number;        // Expiry timestamp (ms)
}
```

## Node

```typescript
interface Node {
  id: number;
  name: string;
  secret: string;
  server_ip: string;
  server_ip_v4?: string;
  server_ip_v6?: string;
  port: string;           // "1000-65535"
  interface_name?: string;
  http: number;           // 1 = enabled
  tls: number;
  socks: number;
  tcp_listen_addr: string;// "[::]"
  udp_listen_addr: string;
  status: number;         // 1 = online, 0 = offline
  is_remote: number;      // 0 = local, 1 = federation
  remote_url?: string;
  remote_token?: string;
  version?: string;
  created_at?: number;
  updated_at?: number;
}

interface NodeCreateRequest {
  name: string;
  serverIp: string;
  serverIpV4?: string;
  serverIpV6?: string;
  port?: string;
  interfaceName?: string;
  http?: number;
  tls?: number;
  socks?: number;
  tcpListenAddr?: string;
  udpListenAddr?: string;
  isRemote?: number;
  remoteUrl?: string;
  remoteToken?: string;
}
```

## Tunnel

```typescript
interface Tunnel {
  id: number;
  name: string;
  type: number;           // 1 = port forward, 2 = tunnel forward
  protocol?: string;
  flow: number;           // Traffic multiplier
  traffic_ratio: number;
  status: number;         // 1 = active, 0 = disabled
  ip_preference?: string; // "ipv4", "ipv6", ""
  in_ip?: string;
  in_node_id?: number[];
  chain_node_id?: number[];
  out_node_id?: number[];
  created_at?: number;
}

interface TunnelCreateRequest {
  name: string;
  type: number;
  flow?: number;
  trafficRatio?: number;
  status?: number;
  ipPreference?: string;
  inIp?: string;
  inNodeId: number[];
  chainNodeId?: number[];
  outNodeId: number[];
}
```

## Forward

```typescript
interface Forward {
  id: number;
  user_id: number;
  tunnel_id: number;
  tunnel_name?: string;
  name: string;
  in_port: number;
  remote_addr: string;
  strategy: string;       // "fifo" | "round"
  status: number;         // 1 = running, 0 = paused
  speed_id: number;
  speed_name?: string;
  in_flow: number;        // Upload bytes
  out_flow: number;       // Download bytes
  created_at?: number;
  updated_at?: number;
}

interface ForwardCreateRequest {
  name: string;
  tunnelId: number;
  remoteAddr: string;
  strategy?: string;
  inPort?: number;
  speedId?: number;
}
```

## Speed Limit

```typescript
interface SpeedLimit {
  id: number;
  name: string;
  speed: number;          // Mbps
  status: number;
  created_at?: number;
}
```

## Groups

```typescript
interface TunnelGroup {
  id: number;
  name: string;
  status: number;
  tunnel_ids?: number[];
  created_at?: number;
}

interface UserGroup {
  id: number;
  name: string;
  status: number;
  user_ids?: number[];
  created_at?: number;
}

interface GroupPermission {
  id: number;
  user_group_id: number;
  user_group_name?: string;
  tunnel_group_id: number;
  tunnel_group_name?: string;
  created_at?: number;
}
```

## User-Tunnel Assignment

```typescript
interface UserTunnel {
  id: number;
  user_id: number;
  tunnel_id: number;
  tunnel_name?: string;
  flow: number;           // Quota for this tunnel in GB
  in_flow: number;
  out_flow: number;
  exp_time: number;
  speed_id: number;
}
```

## Federation

```typescript
interface PeerShare {
  id: number;
  name: string;
  node_id: number;
  node_name?: string;
  token: string;
  max_bandwidth: number;
  expiry_time: number;
  port_range_start: number;
  port_range_end: number;
  allowed_domains: string;
  allowed_ips: string;
  status: number;
  created_at?: number;
}
```

## Backup

```typescript
interface BackupExport {
  version: string;
  exportedAt: number;
  types: string[];
  users?: User[];
  nodes?: Node[];
  tunnels?: Tunnel[];
  forwards?: Forward[];
  speedLimits?: SpeedLimit[];
  tunnelGroups?: TunnelGroup[];
  userGroups?: UserGroup[];
  groupPermissions?: GroupPermission[];
  configs?: Record<string, string>;
}
```

## Client Helper Class

```typescript
class FlvxClient {
  private baseUrl: string;
  private username: string;
  private password: string;
  private token?: string;

  constructor(baseUrl?: string, username?: string, password?: string) {
    this.baseUrl = baseUrl ?? process.env.FLYX_BASE_URL ?? "";
    this.username = username ?? process.env.FLYX_USERNAME ?? "";
    this.password = password ?? process.env.FLYX_PASSWORD ?? "";
  }

  private async ensureToken(): Promise<void> {
    if (this.token) return;
    
    const res = await fetch(`${this.baseUrl}/api/v1/user/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ 
        username: this.username, 
        password: this.password 
      }),
    });
    
    const result: APIResponse<{ token: string }> = await res.json();
    if (result.code !== 0) throw new Error(result.msg);
    this.token = result.data!.token;
  }

  async request<T>(endpoint: string, data?: object): Promise<T> {
    await this.ensureToken();
    
    const res = await fetch(`${this.baseUrl}${endpoint}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": this.token!,  // NO "Bearer " prefix!
      },
      body: JSON.stringify(data ?? {}),
    });
    
    const result: APIResponse<T> = await res.json();
    if (result.code === 401) {
      this.token = undefined;
      return this.request(endpoint, data);
    }
    if (result.code !== 0) throw new Error(result.msg);
    return result.data!;
  }

  // Convenience methods
  async listNodes(): Promise<Node[]> {
    const data = await this.request<{ list: Node[] }>("/api/v1/node/list", {});
    return data.list ?? [];
  }

  async listForwards(): Promise<Forward[]> {
    const data = await this.request<{ list: Forward[] }>("/api/v1/forward/list", {});
    return data.list ?? [];
  }

  async createForward(req: ForwardCreateRequest): Promise<Forward> {
    return this.request("/api/v1/forward/create", req);
  }

  async getUserPackage(): Promise<UserPackage> {
    return this.request("/api/v1/user/package", {});
  }
}
```
