# HTTP Client Examples

Complete, runnable examples for various languages.

## Bash / curl

### Complete Script with Auto-Login

```bash
#!/bin/bash
set -e

# Configuration
BASE_URL="${FLYX_BASE_URL:?FLYX_BASE_URL not set}"
USERNAME="${FLYX_USERNAME:?FLYX_USERNAME not set}"
PASSWORD="${FLYX_PASSWORD:?FLYX_PASSWORD not set}"

# Login and get token
echo "Logging in..."
LOGIN_RESPONSE=$(curl -s -X POST "${BASE_URL}/api/v1/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${USERNAME}\",\"password\":\"${PASSWORD}\"}")

TOKEN=$(echo "$LOGIN_RESPONSE" | jq -r '.data.token // empty')

if [ -z "$TOKEN" ]; then
  echo "Login failed: $(echo "$LOGIN_RESPONSE" | jq -r '.msg')"
  exit 1
fi

echo "Logged in successfully"

# API call helper
api_call() {
  local endpoint="$1"
  local data="${2:-{}}"
  
  curl -s -X POST "${BASE_URL}${endpoint}" \
    -H "Authorization: ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$data"
}

# Examples
echo "=== My Package Info ==="
api_call "/api/v1/user/package" | jq '.'

echo -e "\n=== Node List ==="
api_call "/api/v1/node/list" '{}' | jq '.data.list[] | {name, status: (.status == 1)}'

echo -e "\n=== Forward List ==="
api_call "/api/v1/forward/list" '{}' | jq '.data.list[] | {name, tunnel: .tunnel_name, port: .in_port, target: .remote_addr}'
```

### Create Forward Script

```bash
#!/bin/bash
BASE_URL="${FLYX_BASE_URL}"
TOKEN="${FLYX_TOKEN}"  # Pre-obtained token

create_forward() {
  local name="$1"
  local tunnel_id="$2"
  local remote_addr="$3"
  
  curl -s -X POST "${BASE_URL}/api/v1/forward/create" \
    -H "Authorization: ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{
      \"name\": \"${name}\",
      \"tunnelId\": ${tunnel_id},
      \"remoteAddr\": \"${remote_addr}\",
      \"strategy\": \"fifo\"
    }" | jq '.'
}

# Usage: ./create-forward.sh "my-web" 1 "192.168.1.100:80"
create_forward "$@"
```

---

## Python

### Complete Client Class

```python
#!/usr/bin/env python3
"""FLYX API Client"""

import os
import requests
from typing import Optional, Any, Dict, List

class FlvxError(Exception):
    """FLYX API Error"""
    def __init__(self, code: int, message: str):
        self.code = code
        self.message = message
        super().__init__(message)

class FlvxClient:
    """FLYX API Client with auto-login"""
    
    def __init__(
        self, 
        base_url: Optional[str] = None,
        username: Optional[str] = None,
        password: Optional[str] = None
    ):
        self.base_url = base_url or os.environ.get("FLYX_BASE_URL")
        self.username = username or os.environ.get("FLYX_USERNAME")
        self.password = password or os.environ.get("FLYX_PASSWORD")
        
        if not all([self.base_url, self.username, self.password]):
            raise ValueError("Missing credentials. Set FLYX_BASE_URL, FLYX_USERNAME, FLYX_PASSWORD")
        
        self.token: Optional[str] = None
    
    def _login(self) -> None:
        """Authenticate and store token"""
        resp = requests.post(
            f"{self.base_url}/api/v1/user/login",
            headers={"Content-Type": "application/json"},
            json={"username": self.username, "password": self.password}
        )
        result = resp.json()
        
        if result["code"] != 0:
            raise FlvxError(result["code"], result["msg"])
        
        self.token = result["data"]["token"]
    
    def _headers(self) -> Dict[str, str]:
        """Get request headers with auth"""
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = self.token  # NO "Bearer " prefix!
        return headers
    
    def request(self, endpoint: str, data: Any = None) -> Any:
        """Make authenticated API request"""
        if not self.token:
            self._login()
        
        resp = requests.post(
            f"{self.base_url}{endpoint}",
            headers=self._headers(),
            json=data or {}
        )
        result = resp.json()
        
        if result["code"] == 0:
            return result.get("data")
        
        if result["code"] == 401:
            # Token expired, retry once
            self.token = None
            return self.request(endpoint, data)
        
        raise FlvxError(result["code"], result["msg"])
    
    # Convenience methods
    
    def get_package(self) -> Dict:
        """Get current user's package info"""
        return self.request("/api/v1/user/package", {})
    
    def list_nodes(self) -> List[Dict]:
        """List all nodes"""
        data = self.request("/api/v1/node/list", {})
        return data.get("list", [])
    
    def list_forwards(self, keyword: str = "") -> List[Dict]:
        """List forwards"""
        data = self.request("/api/v1/forward/list", {"keyword": keyword})
        return data.get("list", [])
    
    def create_forward(
        self, 
        name: str, 
        tunnel_id: int, 
        remote_addr: str,
        strategy: str = "fifo",
        speed_id: int = 0
    ) -> Dict:
        """Create a forward"""
        return self.request("/api/v1/forward/create", {
            "name": name,
            "tunnelId": tunnel_id,
            "remoteAddr": remote_addr,
            "strategy": strategy,
            "speedId": speed_id
        })
    
    def pause_forward(self, forward_id: int) -> None:
        """Pause a forward"""
        self.request("/api/v1/forward/pause", {"id": forward_id})
    
    def resume_forward(self, forward_id: int) -> None:
        """Resume a forward"""
        self.request("/api/v1/forward/resume", {"id": forward_id})
    
    def delete_forward(self, forward_id: int) -> None:
        """Delete a forward"""
        self.request("/api/v1/forward/delete", {"id": forward_id})


# Usage example
if __name__ == "__main__":
    client = FlvxClient()
    
    # Get package info
    pkg = client.get_package()
    print(f"Traffic: {pkg['inFlow'] / 1e9:.2f}GB ↑ / {pkg['outFlow'] / 1e9:.2f}GB ↓")
    print(f"Quota: {pkg['flow']}GB")
    
    # List forwards with traffic
    print("\nForwards:")
    for fwd in client.list_forwards():
        print(f"  {fwd['name']}: {fwd['in_port']} → {fwd['remote_addr']}")
        print(f"    Traffic: {fwd['in_flow'] / 1e9:.2f}GB ↑ / {fwd['out_flow'] / 1e9:.2f}GB ↓")
```

---

## Node.js / TypeScript

### Complete Client Class

```typescript
// flyx-client.ts
interface APIResponse<T = unknown> {
  code: number;
  msg: string;
  data?: T;
  ts: number;
}

class FlvxError extends Error {
  constructor(public code: number, message: string) {
    super(message);
    this.name = "FlvxError";
  }
}

interface UserPackage {
  flow: number;
  inFlow: number;
  outFlow: number;
  tunnels: number;
  forwards: number;
  expTime: number;
}

interface Node {
  id: number;
  name: string;
  status: number;
  server_ip: string;
}

interface Forward {
  id: number;
  name: string;
  tunnel_id: number;
  tunnel_name: string;
  in_port: number;
  remote_addr: string;
  status: number;
  in_flow: number;
  out_flow: number;
}

class FlvxClient {
  private baseUrl: string;
  private username: string;
  private password: string;
  private token?: string;

  constructor(options?: {
    baseUrl?: string;
    username?: string;
    password?: string;
  }) {
    this.baseUrl = options?.baseUrl ?? process.env.FLYX_BASE_URL ?? "";
    this.username = options?.username ?? process.env.FLYX_USERNAME ?? "";
    this.password = options?.password ?? process.env.FLYX_PASSWORD ?? "";

    if (!this.baseUrl || !this.username || !this.password) {
      throw new Error("Missing credentials. Set FLYX_BASE_URL, FLYX_USERNAME, FLYX_PASSWORD");
    }
  }

  private async login(): Promise<void> {
    const res = await fetch(`${this.baseUrl}/api/v1/user/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: this.username,
        password: this.password,
      }),
    });

    const result: APIResponse<{ token: string }> = await res.json();
    if (result.code !== 0) {
      throw new FlvxError(result.code, result.msg);
    }

    this.token = result.data!.token;
  }

  private async request<T>(endpoint: string, data?: object): Promise<T> {
    if (!this.token) {
      await this.login();
    }

    const res = await fetch(`${this.baseUrl}${endpoint}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: this.token!, // NO "Bearer " prefix!
      },
      body: JSON.stringify(data ?? {}),
    });

    const result: APIResponse<T> = await res.json();

    if (result.code === 0) {
      return result.data!;
    }

    if (result.code === 401) {
      // Token expired, retry once
      this.token = undefined;
      return this.request<T>(endpoint, data);
    }

    throw new FlvxError(result.code, result.msg);
  }

  // Convenience methods

  async getPackage(): Promise<UserPackage> {
    return this.request("/api/v1/user/package", {});
  }

  async listNodes(): Promise<Node[]> {
    const data = await this.request<{ list: Node[] }>("/api/v1/node/list", {});
    return data.list ?? [];
  }

  async listForwards(keyword = ""): Promise<Forward[]> {
    const data = await this.request<{ list: Forward[] }>("/api/v1/forward/list", {
      keyword,
    });
    return data.list ?? [];
  }

  async createForward(options: {
    name: string;
    tunnelId: number;
    remoteAddr: string;
    strategy?: "fifo" | "round";
    speedId?: number;
  }): Promise<Forward> {
    return this.request("/api/v1/forward/create", {
      name: options.name,
      tunnelId: options.tunnelId,
      remoteAddr: options.remoteAddr,
      strategy: options.strategy ?? "fifo",
      speedId: options.speedId ?? 0,
    });
  }

  async pauseForward(id: number): Promise<void> {
    await this.request("/api/v1/forward/pause", { id });
  }

  async resumeForward(id: number): Promise<void> {
    await this.request("/api/v1/forward/resume", { id });
  }

  async deleteForward(id: number): Promise<void> {
    await this.request("/api/v1/forward/delete", { id });
  }
}

export { FlvxClient, FlvxError };

// Usage
async function main() {
  const client = new FlvxClient();

  // Get package info
  const pkg = await client.getPackage();
  console.log(`Traffic: ${(pkg.inFlow / 1e9).toFixed(2)}GB ↑ / ${(pkg.outFlow / 1e9).toFixed(2)}GB ↓`);
  console.log(`Quota: ${pkg.flow}GB`);

  // List nodes
  console.log("\nNodes:");
  const nodes = await client.listNodes();
  for (const node of nodes) {
    console.log(`  ${node.name}: ${node.status ? "Online" : "Offline"}`);
  }

  // List forwards
  console.log("\nForwards:");
  const forwards = await client.listForwards();
  for (const fwd of forwards) {
    console.log(`  ${fwd.name}: ${fwd.in_port} → ${fwd.remote_addr}`);
  }
}

main().catch(console.error);
```

---

## Go

### Complete Client Package

```go
// flyx/client.go
package flyx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Client struct {
	BaseURL  string
	Username string
	Password string
	Token    string
}

type Response struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
	TS   int64           `json:"ts"`
}

type FlvxError struct {
	Code    int
	Message string
}

func (e *FlvxError) Error() string {
	return fmt.Sprintf("FLYX error %d: %s", e.Code, e.Message)
}

func NewClient() *Client {
	return &Client{
		BaseURL:  os.Getenv("FLYX_BASE_URL"),
		Username: os.Getenv("FLYX_USERNAME"),
		Password: os.Getenv("FLYX_PASSWORD"),
	}
}

func (c *Client) Login() error {
	payload := map[string]string{
		"username": c.Username,
		"password": c.Password,
	}
	
	var result struct {
		Code int `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	
	if err := c.request("/api/v1/user/login", payload, &result); err != nil {
		return err
	}
	
	if result.Code != 0 {
		return &FlvxError{Code: result.Code, Message: result.Msg}
	}
	
	c.Token = result.Data.Token
	return nil
}

func (c *Client) Request(endpoint string, data interface{}, result interface{}) error {
	// Auto-login if no token
	if c.Token == "" {
		if err := c.Login(); err != nil {
			return err
		}
	}
	return c.request(endpoint, data, result)
}

func (c *Client) request(endpoint string, data interface{}, result interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	
	req, err := http.NewRequest("POST", c.BaseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", c.Token) // NO "Bearer " prefix!
	}
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	
	return json.Unmarshal(respBody, result)
}

// Convenience methods

func (c *Client) ListNodes() ([]map[string]interface{}, error) {
	var result struct {
		Code int `json:"code"`
		Data struct {
			List []map[string]interface{} `json:"list"`
		} `json:"data"`
	}
	
	if err := c.Request("/api/v1/node/list", map[string]interface{}{}, &result); err != nil {
		return nil, err
	}
	
	if result.Code != 0 {
		return nil, &FlvxError{Code: result.Code, Message: "failed to list nodes"}
	}
	
	return result.Data.List, nil
}

func (c *Client) CreateForward(name string, tunnelID int, remoteAddr string) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"name":       name,
		"tunnelId":   tunnelID,
		"remoteAddr": remoteAddr,
		"strategy":   "fifo",
	}
	
	var result struct {
		Code int                    `json:"code"`
		Msg  string                 `json:"msg"`
		Data map[string]interface{} `json:"data"`
	}
	
	if err := c.Request("/api/v1/forward/create", payload, &result); err != nil {
		return nil, err
	}
	
	if result.Code != 0 {
		return nil, &FlvxError{Code: result.Code, Message: result.Msg}
	}
	
	return result.Data, nil
}

// Usage example
func Example() {
	client := NewClient()
	
	nodes, err := client.ListNodes()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	
	for _, node := range nodes {
		fmt.Printf("Node: %v (status: %v)\n", node["name"], node["status"])
	}
	
	fwd, err := client.CreateForward("my-forward", 1, "192.168.1.100:80")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	
	fmt.Printf("Created forward: %v\n", fwd)
}
```
