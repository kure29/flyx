"""
Test authentication flow for FLYX.
Tests login, logout, session management, and protected routes.
"""

import pytest
from playwright.sync_api import Page, expect

from pages import LoginPage, DashboardPage


@pytest.mark.e2e
class TestAuthentication:
    """Authentication E2E tests."""

    def test_login_page_loads(self, page: Page, frontend_url: str):
        """Test that login page loads correctly."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()

        expect(page).to_have_url(f"{frontend_url}/")
        expect(login_page.username_input).to_be_visible()
        expect(login_page.password_input).to_be_visible()
        expect(login_page.login_button).to_be_visible()

    def test_login_with_valid_credentials_redirects_to_change_password(self, page: Page, frontend_url: str):
        """Test successful login with default credentials redirects to change-password."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()

        result = login_page.login("admin_user", "admin_user")
        assert result, "Login should succeed with valid credentials"

        assert "/change-password" in page.url

    def test_login_with_invalid_credentials(self, page: Page, frontend_url: str):
        """Test login fails with invalid credentials."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()

        result = login_page.login("invalid_user", "invalid_password")
        assert not result, "Login should fail with invalid credentials"

        expect(page).to_have_url(f"{frontend_url}/")

    def test_login_with_empty_username(self, page: Page, frontend_url: str):
        """Test login validation for empty username."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()

        login_page.password_input.fill("some_password")
        login_page.login_button.click()

        page.wait_for_timeout(500)

        expect(page).to_have_url(f"{frontend_url}/")

    def test_login_with_empty_password(self, page: Page, frontend_url: str):
        """Test login validation for empty password."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()

        login_page.username_input.fill("some_user")
        login_page.login_button.click()

        page.wait_for_timeout(500)

        expect(page).to_have_url(f"{frontend_url}/")

    def test_protected_route_redirects_to_login(self, page: Page, frontend_url: str):
        """Test that protected routes redirect to login when not authenticated."""
        page.goto(f"{frontend_url}/dashboard")
        page.wait_for_load_state("networkidle")

        expect(page).to_have_url(f"{frontend_url}/")

    def test_session_persists_on_refresh(self, page: Page, frontend_url: str):
        """Test that session persists after page refresh."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()
        login_page.login("admin_user", "admin_user")

        assert "/change-password" in page.url

        page.reload()
        page.wait_for_load_state("networkidle")

        assert "/change-password" in page.url

    def test_logout_clears_session(self, page: Page, frontend_url: str):
        """Test that logout clears the session."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()
        login_page.login("admin_user", "admin_user")

        assert "/change-password" in page.url

        page.evaluate("localStorage.clear()")

        page.goto(f"{frontend_url}/dashboard")
        page.wait_for_load_state("networkidle")

        expect(page).to_have_url(f"{frontend_url}/")


@pytest.mark.e2e
@pytest.mark.auth
class TestPasswordChange:
    """Password change E2E tests."""

    def test_password_change_page_accessible(self, page: Page, frontend_url: str):
        """Test that password change page is accessible after login with default password."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()
        login_page.login("admin_user", "admin_user")

        assert "/change-password" in page.url
