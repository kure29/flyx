"""
API client for FLYX backend testing.
"""

import json
from typing import Any, Optional

import requests


class APIClient:
    """API client for FLYX backend."""

    def __init__(self, base_url: str, jwt_secret: Optional[str] = None):
        self.base_url = base_url.rstrip("/")
        self.api_base = f"{self.base_url}/api/v1"
        self.jwt_secret = jwt_secret
        self.token: Optional[str] = None

    def set_token(self, token: str):
        """Set authentication token."""
        self.token = token

    def _headers(self) -> dict[str, str]:
        """Get headers for requests."""
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = self.token
        return headers

    def _request(
        self, method: str, endpoint: str, data: Optional[dict] = None, params: Optional[dict] = None
    ) -> dict[str, Any]:
        """Make HTTP request."""
        url = f"{self.api_base}{endpoint}"
        response = requests.request(
            method=method,
            url=url,
            headers=self._headers(),
            json=data,
            params=params,
            timeout=30,
        )
        try:
            return response.json()
        except json.JSONDecodeError:
            return {"code": -1, "msg": f"Invalid JSON response: {response.text}", "data": None}

    def get(self, endpoint: str, params: Optional[dict] = None) -> dict[str, Any]:
        """GET request."""
        return self._request("GET", endpoint, params=params)

    def post(self, endpoint: str, data: Optional[dict] = None) -> dict[str, Any]:
        """POST request."""
        return self._request("POST", endpoint, data=data)

    def put(self, endpoint: str, data: Optional[dict] = None) -> dict[str, Any]:
        """PUT request."""
        return self._request("PUT", endpoint, data=data)

    def delete(self, endpoint: str, data: Optional[dict] = None) -> dict[str, Any]:
        """DELETE request."""
        return self._request("DELETE", endpoint, data=data)

    def login(self, username: str, password: str, captcha_id: str = "") -> dict[str, Any]:
        """Login and store token."""
        response = self.post(
            "/user/login",
            {"username": username, "password": password, "captchaId": captcha_id},
        )
        if response.get("code") == 0 and response.get("data"):
            self.token = response["data"].get("token")
        return response

    def logout(self):
        """Clear authentication token."""
        self.token = None

    def is_authenticated(self) -> bool:
        """Check if authenticated."""
        if not self.token:
            return False
        response = self.post("/user/package")
        return response.get("code") == 0

    def check_captcha(self) -> bool:
        """Check if captcha is enabled."""
        response = self.post("/captcha/check")
        return response.get("data") == 1

    def get_config(self, name: str) -> Optional[str]:
        """Get config value by name."""
        response = self.post("/config/get", {"name": name})
        if response.get("code") == 0 and response.get("data"):
            return response["data"].get("value")
        return None

    def set_config(self, name: str, value: str) -> bool:
        """Set config value."""
        response = self.post("/config/update-single", {"name": name, "value": value})
        return response.get("code") == 0

    def list_nodes(self) -> list[dict]:
        """List all nodes."""
        response = self.post("/node/list")
        if response.get("code") == 0:
            return response.get("data", [])
        return []

    def create_node(
        self,
        name: str,
        address: str,
        port: int = 8433,
        secret: str = "",
        remark: str = "",
    ) -> dict[str, Any]:
        """Create a new node."""
        return self.post(
            "/node/create",
            {
                "name": name,
                "address": address,
                "port": port,
                "secret": secret,
                "remark": remark,
            },
        )

    def delete_node(self, node_id: int) -> dict[str, Any]:
        """Delete node."""
        return self.post("/node/delete", {"id": node_id})

    def list_tunnels(self) -> list[dict]:
        """List all tunnels."""
        response = self.post("/tunnel/list")
        if response.get("code") == 0:
            return response.get("data", [])
        return []

    def create_tunnel(
        self,
        name: str,
        node_id: int,
        port: int = 0,
        remark: str = "",
        **kwargs,
    ) -> dict[str, Any]:
        """Create a new tunnel."""
        data = {
            "name": name,
            "nodeId": node_id,
            "port": port,
            "remark": remark,
            **kwargs,
        }
        return self.post("/tunnel/create", data)

    def delete_tunnel(self, tunnel_id: int) -> dict[str, Any]:
        """Delete tunnel."""
        return self.post("/tunnel/delete", {"id": tunnel_id})

    def list_forwards(self) -> list[dict]:
        """List all forwards."""
        response = self.post("/forward/list")
        if response.get("code") == 0:
            return response.get("data", [])
        return []

    def create_forward(
        self,
        name: str,
        tunnel_id: int,
        remote_addr: str,
        in_port: int = 0,
        **kwargs,
    ) -> dict[str, Any]:
        """Create a new forward."""
        data = {
            "name": name,
            "tunnelId": tunnel_id,
            "remoteAddr": remote_addr,
            "inPort": in_port,
            **kwargs,
        }
        return self.post("/forward/create", data)

    def delete_forward(self, forward_id: int) -> dict[str, Any]:
        """Delete forward."""
        return self.post("/forward/delete", {"id": forward_id})

    def backup_export(self, types: Optional[list[str]] = None) -> dict[str, Any]:
        """Export backup data."""
        return self.post("/backup/export", {"types": types or []})

    def backup_import(self, backup_data: dict, types: list[str]) -> dict[str, Any]:
        """Import backup data."""
        return self.post("/backup/import", {"types": types, **backup_data})


class TestUser:
    """The panel's only account, the admin, as the tests log in."""

    DEFAULT_ADMIN = ("admin_user", "admin_user")
