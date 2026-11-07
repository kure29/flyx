# FLYX E2E Tests

End-to-end testing suite for FLYX Panel using Playwright and pytest.

## Structure

```
tests/e2e/
├── conftest.py          # Pytest configuration and fixtures
├── pyproject.toml       # Python project configuration
├── with_server.py       # Server lifecycle manager
├── pages/               # Page Object Models
│   └── __init__.py
├── fixtures/            # Test fixtures and helpers
│   └── __init__.py
├── utils/               # Utility modules
│   ├── __init__.py
│   └── api_client.py    # Backend API client
├── test_auth.py         # Authentication tests
├── test_api.py          # API endpoint tests
├── test_dashboard.py    # Dashboard UI tests
└── test_user_ui.py      # User management UI tests
```

## Prerequisites

- Python 3.11+
- Go 1.24+ (for backend)
- Node.js 18+ (for frontend)

## Setup

```bash
# Create virtual environment
cd tests/e2e
python -m venv .venv
source .venv/bin/activate  # or .venv\Scripts\activate on Windows

# Install dependencies
pip install -e ".[dev]"

# Install Playwright browsers
playwright install chromium
```

## Running Tests

### Quick Start

```bash
# Run all tests (starts servers automatically)
python with_server.py -- pytest -v

# Run specific test file
python with_server.py -- pytest test_auth.py -v

# Run with markers
python with_server.py -- pytest -m "auth" -v
python with_server.py -- pytest -m "api" -v
python with_server.py -- pytest -m "e2e" -v
```

### Manual Server Management

If servers are already running:

```bash
# Set environment variables
export E2E_BACKEND_PORT=6365
export E2E_FRONTEND_PORT=3000

# Run tests directly
pytest -v
```

### Custom Server Configuration

```bash
# Custom ports
python with_server.py --backend-port 8080 --frontend-port 5173 -- pytest -v

# Custom server commands
python with_server.py \
  --server "make run" --port 6365 --cwd go-backend \
  --server "pnpm run dev" --port 3000 --cwd vite-frontend \
  -- pytest -v
```

## Test Markers

| Marker    | Description                              |
|-----------|------------------------------------------|
| `@e2e`    | Full end-to-end test with browser        |
| `@api`    | API-only test, no browser required       |
| `@auth`   | Test requires authentication             |
| `@slow`   | Slow running test (>5s)                  |

## Writing Tests

### API Tests

```python
import pytest
from utils.api_client import APIClient

@pytest.mark.api
class TestMyAPI:
    def test_something(self, authenticated_api: APIClient):
        response = authenticated_api.post("/some/endpoint")
        assert response["code"] == 0
```

### Browser Tests

```python
import pytest
from playwright.sync_api import Page
from pages import LoginPage

@pytest.mark.e2e
class TestMyFeature:
    def test_something(self, page: Page, frontend_url: str):
        login_page = LoginPage(page, frontend_url)
        login_page.goto()
        # ...
```

## Page Objects

Located in `pages/__init__.py`:
- `LoginPage` - Login form handling
- `DashboardPage` - Dashboard interactions
- `UserPage` - User management
- `NodePage` - Node management
- `TunnelPage` - Tunnel management
- `ForwardPage` - Forward management
- `ConfigPage` - Configuration

## Fixtures

Key fixtures in `conftest.py`:
- `server_info` - Server configuration
- `backend_url` / `frontend_url` - Base URLs
- `page` - Fresh browser page
- `authenticated_page` - Page with logged-in session
- `api_client` - API client instance
- `authenticated_api` - Authenticated API client
- `auth_token` - JWT token string

## Debugging

```bash
# Run with visible browser
pytest -v --headed

# Run specific test with debug output
pytest test_auth.py::TestAuthentication::test_login_with_valid_credentials -v -s

# Generate HTML report
pytest -v --html=report.html --self-contained-html
```

## CI Integration

```yaml
# Example GitHub Actions
- name: Run E2E tests
  run: |
    cd tests/e2e
    pip install -e ".[dev]"
    playwright install chromium
    python with_server.py -- pytest -v --junit-xml=test-results.xml
```