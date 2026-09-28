from fake_store import FakeStore
import unittest
from pathlib import Path
import httpx
from test_web_service import FakeClient
from web_service import create_app

class DeploymentChecks(unittest.IsolatedAsyncioTestCase):
    async def test_health_and_relative_browser_assets(self):
        app = create_app(FakeClient(), store=FakeStore())
        try:
            async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://localhost") as http:
                health = await http.get("/healthz")
                self.assertEqual(health.json(), {"status": "ok"})
                page = await http.get("/")
                self.assertIn('href="app.css"', page.text)
                self.assertIn('src="app.js"', page.text)
                js = (await http.get("/app.js")).text
                self.assertNotIn("request('/api/", js)
                self.assertIn("request('api/plugins", js)
        finally:
            await app.state.manager.close()
