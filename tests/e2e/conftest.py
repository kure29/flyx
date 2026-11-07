"""
Pytest configuration and fixtures for FLYX E2E tests.
"""

import json
import os
import socket
from pathlib import Path
from typing import Any, Callable, Generator, Optional

import pytest
from playwright.sync_api import APIRequestContext, BrowserContext, Page, Playwright

from utils.api_client import APIClient, TestUser

DEFAULT_BACKEND_PORT = 6365
DEFAULT_FRONTEND_PORT = 3000
DEFAULT_JWT_SECRET = "test-secret-e2e-key-do-not-use-in-production"
DEFAULT_ADMIN_USER = "admin_user"
DEFAULT_ADMIN_PASSWORD = "admin_user"


def get_server_info() -> dict:
    """Get server info from environment or .server_info.json."""
    info_file = Path(__file__).parent / ".server_info.json"
    if info_file.exists():
        with open(info_file) as f:
            return json.load(f)
    return {
        "backend_port": int(os.getenv("E2E_BACKEND_PORT", DEFAULT_BACKEND_PORT)),
        "frontend_port": int(os.getenv("E2E_FRONTEND_PORT", DEFAULT_FRONTEND_PORT)),
        "jwt_secret": os.getenv("E2E_JWT_SECRET", DEFAULT_JWT_SECRET),
    }


@pytest.fixture(scope="session")
def server_info() -> dict:
    """Server configuration info."""
    return get_server_info()


@pytest.fixture(scope="session")
def backend_url(server_info: dict) -> str:
    """Backend API base URL."""
    return f"http://localhost:{server_info['backend_port']}"


@pytest.fixture(scope="session")
def frontend_url(server_info: dict) -> str:
    """Frontend base URL."""
    return f"http://localhost:{server_info['frontend_port']}"


@pytest.fixture(scope="session")
def api_base_url(backend_url: str) -> str:
    """API base URL for APIRequestContext."""
    return f"{backend_url}/api/v1"


@pytest.fixture(scope="session")
def browser_type_launch_args():
    """Browser launch arguments."""
    return {
        "headless": True,
    }


@pytest.fixture(scope="session")
def browser_context_args():
    """Browser context arguments."""
    return {
        "viewport": {"width": 1280, "height": 720},
        "locale": "zh-CN",
    }


@pytest.fixture
def page(context: BrowserContext) -> Generator[Page, None, None]:
    """Create a new page with standard settings."""
    p = context.new_page()
    p.set_default_timeout(10000)
    yield p
    p.close()


@pytest.fixture
def api_client(backend_url: str) -> APIClient:
    """Create API client instance."""
    return APIClient(backend_url)


@pytest.fixture
def authenticated_api(api_client: APIClient) -> APIClient:
    """Create authenticated API client."""
    api_client.login(*TestUser.DEFAULT_ADMIN)
    return api_client


@pytest.fixture
def clean_nodes(authenticated_api: APIClient) -> Generator[Callable[..., dict], None, None]:
    """Clean up test nodes after test."""
    created_ids: list[int] = []

    def _create_node(name: str, address: str = "127.0.0.1", **kwargs: Any) -> dict:
        response = authenticated_api.create_node(name, address, **kwargs)
        if response.get("code") == 0:
            node_id = response.get("data", {}).get("id")
            if node_id:
                created_ids.append(node_id)
        return response

    yield _create_node

    for node_id in created_ids:
        try:
            authenticated_api.delete_node(node_id)
        except Exception:
            pass


@pytest.fixture
def api_context(playwright: Playwright, api_base_url: str) -> Generator[APIRequestContext, None, None]:
    """API request context for testing backend directly."""
    context = playwright.request.new_context(base_url=api_base_url)
    yield context
    context.dispose()


@pytest.fixture
def auth_token(api_context: APIRequestContext) -> Optional[str]:
    """Get authentication token for API calls."""
    response = api_context.post(
        "/user/login",
        data={"username": DEFAULT_ADMIN_USER, "password": DEFAULT_ADMIN_PASSWORD},
    )
    data = response.json()
    if data.get("code") == 0:
        return data.get("data", {}).get("token")
    return None


@pytest.fixture
def fresh_db_path(tmp_path: Path) -> str:
    """Path for a fresh test database."""
    return str(tmp_path / "test.db")


@pytest.fixture(autouse=True)
def skip_if_no_server(server_info: dict):
    """Skip tests if server is not available."""
    backend_port = server_info["backend_port"]
    frontend_port = server_info["frontend_port"]

    for port, name in [(backend_port, "backend"), (frontend_port, "frontend")]:
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                s.settimeout(1)
                s.connect(("localhost", port))
        except OSError:
            pytest.skip(f"{name} server not available on port {port}")


def pytest_configure(config):
    """Configure pytest markers."""
    config.addinivalue_line("markers", "e2e: End-to-end test requiring running servers")
    config.addinivalue_line("markers", "auth: Test requires authentication")
    config.addinivalue_line("markers", "slow: Slow running test")
    config.addinivalue_line("markers", "api: API-only test (no browser needed)")
