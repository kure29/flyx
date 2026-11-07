"""
Page Object Models for FLYX E2E tests.
"""

from typing import Optional

from playwright.sync_api import Page, Locator, expect


class BasePage:
    """Base page object with common functionality."""

    def __init__(self, page: Page, base_url: str):
        self.page = page
        self.base_url = base_url

    def navigate(self, path: str = ""):
        """Navigate to a specific path."""
        url = f"{self.base_url}{path}"
        self.page.goto(url)
        self.page.wait_for_load_state("networkidle")

    def wait_for_url(self, pattern: str, timeout: int = 5000):
        """Wait for URL to match pattern."""
        self.page.wait_for_url(f"**{pattern}**", timeout=timeout)

    def screenshot(self, name: str):
        """Take a screenshot."""
        self.page.screenshot(path=f"/tmp/{name}.png")


class LoginPage(BasePage):
    """Login page object."""

    def __init__(self, page: Page, base_url: str):
        super().__init__(page, base_url)
        self.username_input: Locator = page.locator('input[placeholder="请输入用户名"]')
        self.password_input: Locator = page.locator('input[placeholder="请输入密码"]')
        self.login_button: Locator = page.locator('button:has-text("登录")')
        self.error_toast: Locator = page.locator('[data-testid="toast-error"], .toast-error')

    def goto(self):
        """Navigate to login page."""
        self.navigate("/")

    def login(self, username: str, password: str) -> bool:
        """Perform login action."""
        self.username_input.fill(username)
        self.password_input.fill(password)
        self.login_button.click()

        try:
            self.page.wait_for_url("**/dashboard**", timeout=5000)
            return True
        except Exception:
            try:
                self.page.wait_for_url("**/change-password**", timeout=2000)
                return True
            except Exception:
                return False

    def get_error_message(self) -> Optional[str]:
        """Get error message if present."""
        try:
            toast = self.page.locator('[role="alert"], .toast').first
            if toast.is_visible():
                return toast.text_content()
        except Exception:
            pass
        return None


class DashboardPage(BasePage):
    """Dashboard page object."""

    def __init__(self, page: Page, base_url: str):
        super().__init__(page, base_url)
        self.sidebar: Locator = page.locator("nav, [data-testid='sidebar']")
        self.logout_button: Locator = page.locator('button:has-text("退出"), [data-testid="logout"]')

    def goto(self):
        """Navigate to dashboard."""
        self.navigate("/dashboard")

    def is_authenticated(self) -> bool:
        """Check if user is authenticated on this page."""
        return self.page.url.endswith("/dashboard") or "/dashboard" in self.page.url

    def navigate_to(self, menu_item: str):
        """Navigate to a menu item."""
        self.page.click(f'text="{menu_item}"')
        self.page.wait_for_load_state("networkidle")


class NodePage(BasePage):
    """Node management page object."""

    def __init__(self, page: Page, base_url: str):
        super().__init__(page, base_url)
        self.create_button: Locator = page.locator('button:has-text("创建"), button:has-text("新增")')
        self.node_list: Locator = page.locator("[data-testid='node-list'], table, .node-item")

    def goto(self):
        """Navigate to node management page."""
        self.navigate("/node")

    def get_nodes(self) -> list[str]:
        """Get list of node names."""
        nodes = []
        for item in self.page.locator("tr td:first-child, .node-name").all():
            text = item.text_content()
            if text and text.strip():
                nodes.append(text.strip())
        return nodes


class TunnelPage(BasePage):
    """Tunnel management page object."""

    def __init__(self, page: Page, base_url: str):
        super().__init__(page, base_url)
        self.create_button: Locator = page.locator('button:has-text("创建"), button:has-text("新增")')

    def goto(self):
        """Navigate to tunnel management page."""
        self.navigate("/tunnel")


class ForwardPage(BasePage):
    """Forward management page object."""

    def __init__(self, page: Page, base_url: str):
        super().__init__(page, base_url)
        self.create_button: Locator = page.locator('button:has-text("创建"), button:has-text("新增")')

    def goto(self):
        """Navigate to forward management page."""
        self.navigate("/forward")


class ConfigPage(BasePage):
    """Configuration page object."""

    def __init__(self, page: Page, base_url: str):
        super().__init__(page, base_url)
        self.save_button: Locator = page.locator('button:has-text("保存"), button:has-text("提交")')

    def goto(self):
        """Navigate to config page."""
        self.navigate("/config")
