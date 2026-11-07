"""
Test fixtures for E2E tests.
Reusable test data and setup helpers.
"""

from typing import Any, Callable, Generator

import pytest

from utils.api_client import APIClient, TestUser


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
def clean_tunnels(
    authenticated_api: APIClient, clean_nodes: Callable[..., dict]
) -> Generator[Callable[..., dict], None, None]:
    """Clean up test tunnels after test."""
    created_ids: list[int] = []

    def _create_tunnel(name: str, node_id: int, **kwargs: Any) -> dict:
        response = authenticated_api.create_tunnel(name, node_id, **kwargs)
        if response.get("code") == 0:
            tunnel_id = response.get("data", {}).get("id")
            if tunnel_id:
                created_ids.append(tunnel_id)
        return response

    yield _create_tunnel

    for tunnel_id in created_ids:
        try:
            authenticated_api.delete_tunnel(tunnel_id)
        except Exception:
            pass


@pytest.fixture
def clean_forwards(
    authenticated_api: APIClient,
    clean_tunnels: Callable[..., dict],
    clean_nodes: Callable[..., dict],
) -> Generator[Callable[..., dict], None, None]:
    """Clean up test forwards after test."""
    created_ids: list[int] = []

    def _create_forward(name: str, tunnel_id: int, remote_addr: str, **kwargs: Any) -> dict:
        response = authenticated_api.create_forward(name, tunnel_id, remote_addr, **kwargs)
        if response.get("code") == 0:
            forward_id = response.get("data", {}).get("id")
            if forward_id:
                created_ids.append(forward_id)
        return response

    yield _create_forward

    for forward_id in created_ids:
        try:
            authenticated_api.delete_forward(forward_id)
        except Exception:
            pass
