#!/usr/bin/env python3
"""
Example E2E test script demonstrating Playwright usage.
Run: python with_server.py -- python example_test.py
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright


def test_login_flow():
    """Test basic login flow."""
    import os

    frontend_port = os.getenv("E2E_FRONTEND_PORT", "3000")
    backend_port = os.getenv("E2E_BACKEND_PORT", "6365")

    print(f"Testing frontend at http://localhost:{frontend_port}")
    print(f"Backend API at http://localhost:{backend_port}")

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()
        page.set_default_timeout(10000)

        try:
            page.goto(f"http://localhost:{frontend_port}/")
            page.wait_for_load_state("networkidle")

            print("Login page loaded")

            username_input = page.locator('input[placeholder="请输入用户名"]')
            password_input = page.locator('input[placeholder="请输入密码"]')
            login_button = page.locator('button:has-text("登录")')

            assert username_input.count() > 0, "Username input not found"
            assert password_input.count() > 0, "Password input not found"
            assert login_button.count() > 0, "Login button not found"

            print("Login form elements found")

            username_input.fill("admin_user")
            password_input.fill("admin_user")
            login_button.click()

            page.wait_for_url("**/dashboard**", timeout=5000)
            print("Login successful, redirected to dashboard")

            assert "/dashboard" in page.url, f"Expected dashboard URL, got {page.url}"
            print("Test passed!")

        except Exception as e:
            page.screenshot(path="/tmp/test_failure.png")
            print(f"Test failed: {e}")
            raise
        finally:
            browser.close()


if __name__ == "__main__":
    test_login_flow()
