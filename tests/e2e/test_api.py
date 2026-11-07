"""
Test API endpoints for FLYX backend.
Tests API responses directly without browser.
"""

import pytest

from utils.api_client import APIClient


@pytest.mark.api
class TestAPIHealth:
    """API health check tests."""

    def test_api_endpoint_reachable(self, api_client: APIClient):
        """Test that API endpoint is reachable."""
        response = api_client.post("/captcha/check")
        assert "code" in response

    def test_captcha_check_endpoint(self, api_client: APIClient):
        """Test captcha check endpoint."""
        response = api_client.post("/captcha/check")
        assert response["code"] == 0
        assert "data" in response


@pytest.mark.api
class TestAPIAuthentication:
    """API authentication tests."""

    def test_login_success(self, api_client: APIClient):
        """Test successful login."""
        response = api_client.login("admin_user", "admin_user")
        assert response["code"] == 0
        assert "token" in response["data"]
        assert api_client.token is not None

    def test_login_invalid_user(self, api_client: APIClient):
        """Test login with invalid user."""
        response = api_client.login("nonexistent", "password")
        assert response["code"] != 0

    def test_login_invalid_password(self, api_client: APIClient):
        """Test login with invalid password."""
        response = api_client.login("admin_user", "wrong_password")
        assert response["code"] != 0

    def test_login_empty_username(self, api_client: APIClient):
        """Test login with empty username."""
        response = api_client.login("", "password")
        assert response["code"] != 0

    def test_login_empty_password(self, api_client: APIClient):
        """Test login with empty password."""
        response = api_client.login("admin_user", "")
        assert response["code"] != 0

    def test_protected_endpoint_without_token(self, api_client: APIClient):
        """Test that protected endpoint rejects requests without token."""
        response = api_client.post("/tunnel/list")
        assert response["code"] == 401

    def test_protected_endpoint_with_token(self, authenticated_api: APIClient):
        """Test that protected endpoint accepts requests with token."""
        response = authenticated_api.post("/tunnel/list")
        assert response["code"] == 0
        assert isinstance(response["data"], list)


@pytest.mark.api
class TestAPIConfig:
    """API configuration tests."""

    def test_get_configs(self, authenticated_api: APIClient):
        """Test getting all configs."""
        response = authenticated_api.post("/config/list")
        assert response["code"] == 0
        assert isinstance(response["data"], dict)

    def test_get_single_config(self, authenticated_api: APIClient):
        """Test getting a single config."""
        response = authenticated_api.post("/config/get", {"name": "app_name"})
        if response["code"] == 0:
            assert "value" in response["data"]


@pytest.mark.api
class TestAPINodeManagement:
    """API node management tests."""

    def test_list_nodes(self, authenticated_api: APIClient):
        """Test listing nodes."""
        response = authenticated_api.post("/node/list")
        assert response["code"] == 0
        assert isinstance(response["data"], list)


@pytest.mark.api
class TestAPITunnelManagement:
    """API tunnel management tests."""

    def test_list_tunnels(self, authenticated_api: APIClient):
        """Test listing tunnels."""
        response = authenticated_api.post("/tunnel/list")
        assert response["code"] == 0
        assert isinstance(response["data"], list)


@pytest.mark.api
class TestAPIForwardManagement:
    """API forward management tests."""

    def test_list_forwards(self, authenticated_api: APIClient):
        """Test listing forwards."""
        response = authenticated_api.post("/forward/list")
        assert response["code"] == 0
        assert isinstance(response["data"], list)


@pytest.mark.api
class TestAPIBackup:
    """API backup tests."""

    def test_backup_export(self, authenticated_api: APIClient):
        """Test backup export."""
        response = authenticated_api.backup_export()
        assert "version" in response, f"Expected version in backup response: {response}"
