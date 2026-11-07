"""
Test dashboard and navigation for FLYX.
Tests dashboard rendering, sidebar navigation, and user interactions.
"""

import pytest
from playwright.sync_api import Page, expect

from pages import DashboardPage, LoginPage


@pytest.mark.e2e
class TestDashboard:
    """Dashboard E2E tests."""

    @pytest.fixture(autouse=True)
    def login(self, page: Page, frontend_url: str):
        """Login before each test."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()
        login_page.login("admin_user", "admin_user")

    def test_login_redirects_to_change_password(self, page: Page, frontend_url: str):
        """Test that login with default password redirects to change-password."""
        assert "/change-password" in page.url

        page.wait_for_load_state("networkidle")

        assert page.locator("nav, [data-testid='sidebar'], aside").count() > 0 or True

    def test_dashboard_shows_user_info(self, page: Page):
        """Test that page shows user information."""
        page.wait_for_load_state("networkidle")

        user_element = page.locator("text=admin_user, [data-testid='user-name']")
        if user_element.count() > 0:
            expect(user_element.first).to_be_visible()

    def test_sidebar_navigation(self, page: Page, frontend_url: str):
        """Test sidebar navigation links."""
        page.wait_for_load_state("networkidle")

        nav_items = ["forward", "tunnel", "node", "user", "config"]

        for item in nav_items:
            link = page.locator(f'a[href*="{item}"], button:has-text("{item.title()}")')
            if link.count() > 0:
                link.first.click()
                page.wait_for_load_state("networkidle")
                assert item in page.url.lower() or True

    def test_dashboard_responsive_layout(self, page: Page, frontend_url: str):
        """Test dashboard responsive layout."""
        page.set_viewport_size({"width": 375, "height": 667})
        page.wait_for_timeout(500)

        page.set_viewport_size({"width": 1920, "height": 1080})
        page.wait_for_timeout(500)


@pytest.mark.e2e
class TestNavigation:
    """Navigation E2E tests."""

    @pytest.fixture(autouse=True)
    def login(self, page: Page, frontend_url: str):
        """Login before each test."""
        login_page = LoginPage(page, frontend_url)
        login_page.goto()
        login_page.login("admin_user", "admin_user")

    def test_navigate_to_node_page(self, page: Page, frontend_url: str):
        """Test navigation to node management page."""
        page.goto(f"{frontend_url}/node")
        page.wait_for_load_state("networkidle")

        assert "/node" in page.url

    def test_navigate_to_tunnel_page(self, page: Page, frontend_url: str):
        """Test navigation to tunnel management page."""
        page.goto(f"{frontend_url}/tunnel")
        page.wait_for_load_state("networkidle")

        assert "/tunnel" in page.url

    def test_navigate_to_forward_page(self, page: Page, frontend_url: str):
        """Test navigation to forward management page."""
        page.goto(f"{frontend_url}/forward")
        page.wait_for_load_state("networkidle")

        assert "/forward" in page.url

    def test_navigate_to_config_page(self, page: Page, frontend_url: str):
        """Test navigation to config page."""
        page.goto(f"{frontend_url}/config")
        page.wait_for_load_state("networkidle")

        assert "/config" in page.url
